package main

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/caneroj1/stemmer"
	"rsc.io/pdf"
)

var (
	// spacingCoefficient multipied by font size determines if
	// two consecutive letters are in the same word.
	spacingCoefficient float64

	// disableWordsCheck toggles the check for words in dictionary.
	disableWordsCheck bool

	// wordsInDictPercent is the percentage of words in a string
	// that must be dictionary words for the string to be a valid title.
	wordsInDictPercent float64

	// gsCmd points to the ghoscript executable.
	gsCmd string

	// wordsList is a list of words per line.
	// It currently uses the ones from NetBSD dict 
	//go:embed words
	wordsList string

	// words is wordsList as a set.
	words map[string]bool = make(map[string]bool)

	// wordsExtractor is used to extract words from strings.
	wordsExtractor = regexp.MustCompile("[[:alpha:]]{3,30}")
)

func usage() {
	fmt.Fprint(os.Stderr, `usage: pdftitle file..

Pdftitle prints the title of each pdf file.

Flags:
`)
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	flag.Float64Var(&spacingCoefficient, "s", 0.16, "spacing coefficient used to decided word boundaries")
	flag.BoolVar(&disableWordsCheck, "w", false, "disable dictionary check")
	flag.Float64Var(&wordsInDictPercent, "p", 0.20, "minimum percentage of words in dictionary for a valid title")
	flag.StringVar(&gsCmd, "gs", "gs", "ghostscript exec")
	flag.Usage = usage
	flag.Parse()

	if !disableWordsCheck {
		for w := range strings.Lines(wordsList) {
			words[strings.ToLower(strings.TrimRight(w, "\n"))] = true
		}
	}

	for _, fname := range flag.Args() {
		tl, err := title(fname)
		if err == nil {
			fmt.Fprintf(os.Stdout, "%s: %s\n", fname, tl)
		} else {
			fmt.Fprintf(os.Stderr, "error: %s: %v\n", fname, err)
		}
	}
}

// title tries to extract the pdf title of file.
func title(fname string) (string, error) {
	phrases, err := phrasesOfDoc(func() (*pdf.Reader, error) {
		return pdf.Open(fname)
	})
	if err == nil {
		return titleFromPhrases(phrases), nil
	}

	// the pdf package cannot read zipped deflated encoded pdf
	// so we use gs to convert.
	if !strings.Contains(err.Error(), "stream not present") {
		return "", err
	}
	pdfdec, err := decodedWithGhostscript(fname)
	if err != nil {
		return "", err
	}

	phrases, err = phrasesOfDoc(func() (*pdf.Reader, error) {
		return pdf.NewReader(bytes.NewReader(pdfdec.Bytes()), int64(pdfdec.Len()))
	})
	if err == nil {
		return titleFromPhrases(phrases), nil
	}

	return "", err
}

// phrasesOfDoc extracts the phrases of document.
// We pass the document with a builder func to handle pdf reader
// panics in one place.
func phrasesOfDoc(docgen func() (*pdf.Reader, error)) (phrases []*phrase, rerr error) {
	defer func() {
		if val := recover(); val != nil {
			// do not send garbage to output
			var errStr string
			if err, ok := val.(error); ok {
				errStr = err.Error()
			} else {
				errStr = fmt.Sprint(val)
			}
			if i := strings.Index(errStr, "malformed hex string"); i >= 0 {
				rerr = errors.New("reader paniced: malformed hex string")
			} else {
				rerr = fmt.Errorf("reader paniced: %s", errStr)
			}
		}
	}()

	doc, err := docgen()
	if err != nil {
		return nil, fmt.Errorf("can't init reader: %w", err)
	}

	var firstPage pdf.Page
	for i := 1; i <= doc.NumPage(); i++ {
		if p := doc.Page(i); !p.V.IsNull() {
			firstPage = p
			break
		}
	}
	if firstPage.V.IsNull() {
		return nil, nil
	}

	var currPhrase *phrase
	for _, t := range firstPage.Content().Text {
		if currPhrase == nil {
			currPhrase = newPhrase(t)
		} else if !currPhrase.tryAppend(t) {
			phrases = append(phrases, currPhrase)
			currPhrase = newPhrase(t)
		}
	}
	if currPhrase != nil {
		phrases = append(phrases, currPhrase)
	}

	if len(phrases) == 0 {
		return nil, nil
	}
	return
}

// titleFromPhrases tries to guess which of the phrases is the document title.
func titleFromPhrases(phrases []*phrase) string {
	// sort by decreasing font size. We expect the title to be the phrase
	// with the largest font size unless it is very short.
	// The most common case is a text paragraph after the title
	// that starts with a very big letter.
	slices.SortFunc(phrases, func(a, b *phrase) int {
		return cmp.Compare(b.fontSize, a.fontSize)
	})

	if len(phrases) == 0 {
		return ""
	}
	tl := phrases[0].String()
	if len(tl) < 4 {
		if len(phrases) > 1 {
			tl = phrases[1].String()
		} else {
			tl = ""
		}
	}

	if disableWordsCheck || dictCheck(tl) {
		return tl
	}
	return ""
}

// phrase represents a list of words that probably form a single phrase.
// Phrases are defined loosely by checking letter font properties.
type phrase struct {
	font     string
	fontSize float64
	spacing  float64
	prevx    float64
	prevy    float64
	length   int
	b        strings.Builder
}

// newPhrases returns a new phrase starting with t.
func newPhrase(t pdf.Text) *phrase {
	p := &phrase{
		font:     t.Font,
		fontSize: t.FontSize,
		spacing:  spacingCoefficient * t.FontSize,
	}
	p.b.WriteString(printable(t.S))
	p.length += len(t.S)
	p.prevx = t.X + t.W
	p.prevy = t.Y
	return p
}

// tryAppend tries to add t to the phrase and returns true if successful.
func (p *phrase) tryAppend(t pdf.Text) bool {
	// after some tests, it seems that if we are a bit loose with
	// font names and sizes we can do better. Presentation slides
	// use many fonts and both upper and lower case letters.
	// Technical articles use standard fonts so names do not matter
	fontFits := true
	fontSizeFits := math.Abs(t.FontSize-p.fontSize) < 4.0
	canAppend := fontSizeFits && fontFits
	if !canAppend {
		return false
	}

	// do not add the separator at the beginning
	if p.length > 0 {
		if t.Y < p.prevy || t.X-p.prevx >= p.spacing {
			p.b.WriteString(" ")
			p.length++
		}
	}
	p.b.WriteString(printable(t.S))
	p.length += len(t.S)
	p.prevx = t.X + t.W
	p.prevy = t.Y
	return true
}

// String returns the phrase as a single string.
func (p *phrase) String() string {
	// trim for the cases it misses the title and
	// returns the document full text
	s := strings.Join(strings.Fields(p.b.String()), " ")
	return s[0:min(80, len(s))]
}

// dictCheck returns true if s contains enough dictionary words.
func dictCheck(s string) bool {
	tlwords := 0
	tlwordsInDict := 0
	for _, w := range wordsExtractor.FindAllString(s, -1) {
		// stemmer is very aggressive, for example it outputs
		// decline->declin, computers->comput.
		// Best to check both original word and stemmed.
		if words[strings.ToLower(w)] || words[strings.ToLower(stemmer.Stem(w))] {
			tlwordsInDict++
		}
		tlwords++
	}
	return tlwords > 0 &&
		float64(tlwordsInDict)/float64(tlwords) >= wordsInDictPercent
}

// printable returns a copy of s where all non printable characters
// are replaced by a space.
func printable(s string) string {
	const space = rune(32)

	runes := make([]rune, 0)
	for {
		r, siz := utf8.DecodeRuneInString(s)
		if siz == 0 {
			break
		}
		if r == utf8.RuneError {
			runes = append(runes, space)
		} else if unicode.IsGraphic(r) {
			runes = append(runes, r)
		} else {
			runes = append(runes, space)
		}
		s = s[siz:]
	}
	return string(runes)
}

// decodedWithGhostscript runs ghostscript to produce a deflated, uncompressed pdf.
func decodedWithGhostscript(fname string) (*bytes.Buffer, error) {
	fout := bytes.NewBuffer(make([]byte, 0, 10*1024*1024))

	args := []string{
		"-dNOPAUSE",
		"-dBATCH",
		"-dSAFER",
		"-dQUIET",
		"-sDEVICE=pdfwrite",
		"-sOutputFile=-",
		"-dFirstPage=1",
		"-dLastPage=1",
		fname,
	}

	ctx, cancelFunc := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancelFunc()

	cmd := exec.CommandContext(ctx, gsCmd, args...)
	cmd.Stdout = fout
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to transform %q: %w", fname, err)
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("failed to transform %q: %w", fname, err)
	}
	return fout, nil
}
