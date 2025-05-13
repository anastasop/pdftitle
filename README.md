# Pdftitle

Pdftitle is a simple program that tries to extract the titles of pdf documents.
It is used as a simple search tool like `pdftitle pdf/* | grep -i <term>`.
It works using heuristics on font sizes and names, nothing fancy like ML or CV.

## Installation

Pdftitle is written in [go](https://go.dev) and is tested with go >= 1.23.
It also needs ghostscript to transform compressed pdfs. On unix system it is
probably already installed, if not use your package manager to get it.

To install pdftitle use the go tool.

`go install github.com/anastasop/pdftitle@latest`

## Usage

Straightforward

```
$ pdftitle /home/anastasop/pdf/*
/home/anastasop/pdf/19720005243.pdf: WHAT MADE APOLLO A SUCCESS?
/home/anastasop/pdf/200979.201000.pdf: Repository Mirroring
/home/anastasop/pdf/3386319.pdf: 
/home/anastasop/pdf/361598.361623.pdf: On the Criteria To Be Used in Decomposing Systems into Modules
/home/anastasop/pdf/44875.pdf: Achieving Rapid Response Times in Large Online Services
/home/anastasop/pdf/Brin98Anatomy.pdf: The anatomyof a large-scalehypertextualWebsearchengine
/home/anastasop/pdf/FFS.pdf: A Fast File System for UNIX* Marshall Kirk McKusick, William N. Joy, Samuel J.
/home/anastasop/pdf/Martin Gardner - The Colossal Book Of Mathematics.pdf: THE COLOSSAL BOOK OF MATHEMATICS
/home/anastasop/pdf/rsc-deps.pdf: Surviving Software Dependencies
/home/anastasop/pdf/rsync-cheat-sheet.pdf: 
```

It outputs the filename and the title if possible. You can see that because it uses heuristics sometimes
it cannot get word spacing right or the title includes some text following the title.

## Bugs

The pdf reader it uses is no longer actively maintained but works well and is simple enough.
For now i will stick with it and don't move to a fancier reader.

## License

Pdftitle is released under the GNU public license version 3.
