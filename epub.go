/*
Package epub generates valid EPUB 3.0 files with additional EPUB 2.0 table of
contents for maximum compatibility.

Basic usage:

	// Create a new EPUB
	e, err := epub.NewEpub("My title")
	if err != nil {
		log.Println(err)
	}


	// Set the author
	e.SetAuthor("Hingle McCringleberry")

	// Add a section
	section1Body := `<h1>Section 1</h1>
	<p>This is a paragraph.</p>`
	e, err := e.AddSection(section1Body, "Section 1", "", "")
	if err != nil {
		log.Println(err)
	}

	// Write the EPUB
	err = e.Write("My EPUB.epub")
	if err != nil {
		// handle error
	}
*/
package epub

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/gofrs/uuid/v5"
	"github.com/vincent-petithory/dataurl"
)

// FilenameAlreadyUsedError is thrown by AddCSS, AddFont, AddImage, or AddSection
// if the same filename is used more than once.
type FilenameAlreadyUsedError struct {
	Filename string // Filename that caused the error
}

func (e *FilenameAlreadyUsedError) Error() string {
	return fmt.Sprintf("Filename already used: %s", e.Filename)
}

// FileRetrievalError is thrown by AddCSS, AddFont, AddImage, or Write if there was a
// problem retrieving the source file that was provided.
type FileRetrievalError struct {
	Source string // The source of the file whose retrieval failed
	Err    error  // The underlying error that was thrown
}

func (e *FileRetrievalError) Error() string {
	return fmt.Sprintf("Error retrieving %q from source: %+v", e.Source, e.Err)
}

// ParentDoesNotExistError is thrown by AddSubSection if the parent with the
// previously defined internal filename does not exist.
type ParentDoesNotExistError struct {
	Filename string // Filename that caused the error
}

func (e *ParentDoesNotExistError) Error() string {
	return fmt.Sprintf("Parent with the internal filename %s does not exist", e.Filename)
}

// Folder names used for resources inside the EPUB
const (
	CSSFolderName   = "css"
	FontFolderName  = "fonts"
	ImageFolderName = "images"
	VideoFolderName = "videos"
	AudioFolderName = "audios"
)

const (
	cssFileFormat          = "css%04d%s"
	defaultCoverBody       = `<img src="%s" alt="Cover Image" />`
	defaultCoverCSSContent = `body {
  background-color: #FFFFFF;
  margin-bottom: 0px;
  margin-left: 0px;
  margin-right: 0px;
  margin-top: 0px;
  text-align: center;
}
img {
  max-height: 100%;
  max-width: 100%;
}
`
	defaultCoverCSSFilename   = "cover.css"
	defaultCoverCSSSource     = "cover.css"
	defaultCoverImgFormat     = "cover%s"
	defaultCoverXhtmlFilename = "cover.xhtml"
	defaultEpubLang           = "en"
	fontFileFormat            = "font%04d%s"
	imageFileFormat           = "image%04d%s"
	videoFileFormat           = "video%04d%s"
	sectionFileFormat         = "section%04d.xhtml"
	urnUUIDPrefix             = "urn:uuid:"
	audioFileFormat           = "audio%04d%s"
)

// Epub implements an EPUB file.
type Epub struct {
	sync.Mutex
	*http.Client
	author string
	cover  *epubCover
	// The key is the css filename, the value is the css source
	css map[string]string
	// The key is the font filename, the value is the font source
	fonts      map[string]string
	identifier string
	// The key is the image filename, the value is the image source
	images map[string]string
	// The key is the video filename, the value is the video source
	videos map[string]string
	// The key is the audio filename, the value is the audio source
	audios map[string]string
	// Language
	lang string
	// Description
	desc string
	// Page progression direction
	ppd string
	// The package file (package.opf)
	pkg      *pkg
	sections []*epubSection
	title    string
	// Table of contents
	toc *toc
}

type epubCover struct {
	cssFilename   string
	cssTempFile   string
	imageFilename string
	xhtmlFilename string
}

type epubSection struct {
	filename   string
	xhtml      *xhtml
	children   []*epubSection
	properties string
}

// NewEpub returns a new Epub.
func NewEpub(title string) (*Epub, error) {
	var err error
	e := &Epub{}
	e.cover = &epubCover{
		cssFilename:   "",
		cssTempFile:   "",
		imageFilename: "",
		xhtmlFilename: "",
	}
	e.Client = http.DefaultClient
	e.css = make(map[string]string)
	e.fonts = make(map[string]string)
	e.images = make(map[string]string)
	e.videos = make(map[string]string)
	e.audios = make(map[string]string)
	e.pkg, err = newPackage()
	if err != nil {
		return nil, fmt.Errorf("can't create NewEpub: %w", err)
	}
	e.toc, err = newToc()
	if err != nil {
		return nil, fmt.Errorf("can't create NewEpub: %w", err)
	}
	// Set minimal required attributes
	e.SetIdentifier(urnUUIDPrefix + uuid.Must(uuid.NewV4()).String())
	e.SetLang(defaultEpubLang)
	e.SetTitle(title)

	return e, nil
}

// AddCSS adds a CSS file to the EPUB and returns a relative path to the CSS
// file that can be used in EPUB sections in the format:
// ../CSSFolderName/internalFilename
//
// The CSS source should either be a URL, a path to a local file, or an embedded data URL; in any
// case, the CSS file will be retrieved and stored in the EPUB.
//
// The internal filename will be used when storing the CSS file in the EPUB
// and must be unique among all CSS files. If the same filename is used more
// than once, FilenameAlreadyUsedError will be returned. The internal filename is
// optional; if no filename is provided, one will be generated.
func (e *Epub) AddCSS(source string, internalFilename string) (string, error) {
	e.Lock()
	defer e.Unlock()
	return e.addCSS(source, internalFilename)
}

func (e *Epub) addCSS(source string, internalFilename string) (string, error) {
	return addMedia(e.Client, source, internalFilename, cssFileFormat, CSSFolderName, e.css)
}

// AddFont adds a font file to the EPUB and returns a relative path to the font
// file that can be used in EPUB sections in the format:
// ../FontFolderName/internalFilename
//
// The font source should either be a URL, a path to a local file, or an embedded data URL; in any
// case, the font file will be retrieved and stored in the EPUB.
//
// The internal filename will be used when storing the font file in the EPUB
// and must be unique among all font files. If the same filename is used more
// than once, FilenameAlreadyUsedError will be returned. The internal filename is
// optional; if no filename is provided, one will be generated.
func (e *Epub) AddFont(source string, internalFilename string) (string, error) {
	e.Lock()
	defer e.Unlock()
	return addMedia(e.Client, source, internalFilename, fontFileFormat, FontFolderName, e.fonts)
}

// AddImage adds an image to the EPUB and returns a relative path to the image
// file that can be used in EPUB sections in the format:
// ../ImageFolderName/internalFilename
//
// The image source should either be a URL, a path to a local file, or an embedded data URL; in any
// case, the image file will be retrieved and stored in the EPUB.
//
// The internal filename will be used when storing the image file in the EPUB
// and must be unique among all image files. If the same filename is used more
// than once, FilenameAlreadyUsedError will be returned. The internal filename is
// optional; if no filename is provided, one will be generated.
func (e *Epub) AddImage(source string, imageFilename string) (string, error) {
	e.Lock()
	defer e.Unlock()
	return addMedia(e.Client, source, imageFilename, imageFileFormat, ImageFolderName, e.images)
}

// AddVideo adds an video to the EPUB and returns a relative path to the video
// file that can be used in EPUB sections in the format:
// ../VideoFolderName/internalFilename
//
// The video source should either be a URL, a path to a local file, or an embedded data URL; in any
// case, the video file will be retrieved and stored in the EPUB.
//
// The internal filename will be used when storing the video file in the EPUB
// and must be unique among all video files. If the same filename is used more
// than once, FilenameAlreadyUsedError will be returned. The internal filename is
// optional; if no filename is provided, one will be generated.
func (e *Epub) AddVideo(source string, videoFilename string) (string, error) {
	e.Lock()
	defer e.Unlock()
	return addMedia(e.Client, source, videoFilename, videoFileFormat, VideoFolderName, e.videos)
}

// AddAudio adds an audio to the EPUB and returns a relative path to the audio
// file that can be used in EPUB sections in the format:
// ../AudioFolderName/internalFilename
//
// The audio source should either be a URL, a path to a local file, or an embedded data URL; in any
// case, the audio file will be retrieved and stored in the EPUB.
//
// The internal filename will be used when storing the audio file in the EPUB
// and must be unique among all audio files. If the same filename is used more
// than once, FilenameAlreadyUsedError will be returned. The internal filename is
// optional; if no filename is provided, one will be generated.
func (e *Epub) AddAudio(source string, audioFilename string) (string, error) {
	e.Lock()
	defer e.Unlock()
	return addMedia(e.Client, source, audioFilename, audioFileFormat, AudioFolderName, e.audios)
}

// AddSection adds a new section (chapter, etc) to the EPUB and returns a
// relative path to the section that can be used from another section (for
// links).
//
// The body must be valid XHTML that will go between the <body> tags of the
// section XHTML file. The content will not be validated.
//
// The title will be used for the table of contents. The section will be shown
// in the table of contents in the same order it was added to the EPUB. The
// title is optional; if no title is provided, the section will not be added to
// the table of contents.
//
// The internal filename will be used when storing the section file in the EPUB
// and must be unique among all section files. If the same filename is used more
// than once, FilenameAlreadyUsedError will be returned. The internal filename is
// optional; if no filename is provided, one will be generated.
//
// The internal path to an already-added CSS file (as returned by AddCSS) to be
// used for the section is optional.
func (e *Epub) AddSection(body string, sectionTitle string, internalFilename string, internalCSSPath string) (string, error) {
	e.Lock()
	defer e.Unlock()
	return e.addSection("", body, sectionTitle, internalFilename, internalCSSPath)
}

// AddSubSection adds a nested section (chapter, etc) to an existing section.
// The method returns a relative path to the section that can be used from another
// section (for links).
//
// The parent filename must be a valid filename from another section already added.
//
// The body must be valid XHTML that will go between the <body> tags of the
// section XHTML file. The content will not be validated.
//
// The title will be used for the table of contents. The section will be shown
// as a nested entry of the parent section in the table of contents. The
// title is optional; if no title is provided, the section will not be added to
// the table of contents.
//
// The internal filename will be used when storing the section file in the EPUB
// and must be unique among all section files. If the same filename is used more
// than once, FilenameAlreadyUsedError will be returned. The internal filename is
// optional; if no filename is provided, one will be generated.
//
// The internal path to an already-added CSS file (as returned by AddCSS) to be
// used for the section is optional.
func (e *Epub) AddSubSection(parentFilename string, body string, sectionTitle string, internalFilename string, internalCSSPath string) (string, error) {
	e.Lock()
	defer e.Unlock()
	return e.addSection(parentFilename, body, sectionTitle, internalFilename, internalCSSPath)
}

func (e *Epub) addSection(parentFilename string, body string, sectionTitle string, internalFilename string, internalCSSPath string) (string, error) {

	// get list of all xhtml filename inside of epub
	filenamelist := getFilenames(e.sections)
	parentIndex := filenamelist[parentFilename] - 1

	if parentFilename != "" && parentIndex == -1 {
		return "", &ParentDoesNotExistError{Filename: parentFilename}
	}

	// Generate a filename if one isn't provided
	if internalFilename == "" {
		index := 1
		for internalFilename == "" {
			internalFilename = fmt.Sprintf(sectionFileFormat, index)
			if keyExists(filenamelist, internalFilename) {
				internalFilename, index = "", index+1
			}
		}
	} else {
		// if internalFilename is not empty, check that it has .xhtml at the end.
		// if it doesn't have add .xhtml at the end
		// than if it is duplicate return error
		if filepath.Ext(internalFilename) != ".xhtml" {
			internalFilename += ".xhtml"
		}
		if keyExists(filenamelist, internalFilename) {
			return "", &FilenameAlreadyUsedError{Filename: internalFilename}
		}
	}

	x, err := newXhtml(body)
	if err != nil {
		return internalFilename, fmt.Errorf("can't add section we cant create xhtml: %w", err)
	}
	x.setTitle(sectionTitle)
	x.setXmlnsEpub(xmlnsEpub)

	if internalCSSPath != "" {
		x.setCSS(internalCSSPath)
	}

	s := &epubSection{
		filename:   internalFilename,
		xhtml:      x,
		children:   nil,
		properties: propertiesFromBody(body),
	}

	// section have parentIndex -1 and subsection have parrentindex != -1
	if parentIndex == -1 {
		// if it is section append to the root
		e.sections = append(e.sections, s)
	} else {
		// find parent section and append subsection to that
		err := sectionAppender(e.sections, parentFilename, s)
		if err != nil {
			return "", err
		}
	}

	return internalFilename, nil
}

// supports mathml, svg, scripted
// does not support remote-sources, switch (deprecated)
func propertiesFromBody(body string) string {
	prop := map[string]bool{}

	decoder := xml.NewDecoder(bytes.NewBufferString(body))
	for {
		t, _ := decoder.Token()
		if t == nil {
			break
		}
		switch se := t.(type) {
		case xml.StartElement:
			switch strings.ToUpper(se.Name.Local) {
			case "SVG":
				prop["svg"] = true
			case "MATH":
				if se.Name.Space == "http://www.w3.org/1998/Math/MathML" {
					prop["mathml"] = true
				}
			case "SCRIPT":
				prop["scripted"] = true
				// See the comment in TestSectionProperties
				//case "FORM":
				//	prop["scripted"] = true
			}
		default:
		}
	}

	ret := []string{}
	for k := range prop {
		ret = append(ret, k)
	}
	return strings.Join(ret, " ")
}

// Author returns the author of the EPUB.
func (e *Epub) Author() string {
	return e.author
}

// Identifier returns the unique identifier of the EPUB.
func (e *Epub) Identifier() string {
	return e.identifier
}

// Lang returns the language of the EPUB.
func (e *Epub) Lang() string {
	return e.lang
}

// Description returns the description of the EPUB.
func (e *Epub) Description() string {
	return e.desc
}

// Ppd returns the page progression direction of the EPUB.
func (e *Epub) Ppd() string {
	return e.ppd
}

// SetAuthor sets the author of the EPUB.
func (e *Epub) SetAuthor(author string) {
	e.Lock()
	defer e.Unlock()
	e.author = author
	e.pkg.setAuthor(author)
}

// SetCover sets the cover page for the EPUB using the provided image source and
// optional CSS.
//
// The internal path to an already-added image file (as returned by AddImage) is
// required.
//
// The internal path to an already-added CSS file (as returned by AddCSS) to be
// used for the cover is optional. If the CSS path isn't provided, default CSS
// will be used.
func (e *Epub) SetCover(internalImagePath string, internalCSSPath string) error {
	e.Lock()
	defer e.Unlock()
	// If a cover already exists
	if e.cover.xhtmlFilename != "" {
		// Remove the xhtml file
		for i, section := range e.sections {
			if section.filename == e.cover.xhtmlFilename {
				e.sections = append(e.sections[:i], e.sections[i+1:]...)
				break
			}
		}

		// Remove the image
		delete(e.images, e.cover.imageFilename)

		// Remove the CSS
		delete(e.css, e.cover.cssFilename)

		if e.cover.cssTempFile != "" {
			os.Remove(e.cover.cssTempFile)
		}
	}

	e.cover.imageFilename = filepath.Base(internalImagePath)
	e.pkg.setCover(e.cover.imageFilename)

	// Use default cover stylesheet if one isn't provided
	if internalCSSPath == "" {
		// Encode the default CSS
		e.cover.cssTempFile = dataurl.EncodeBytes([]byte(defaultCoverCSSContent))
		var err error
		internalCSSPath, err = e.addCSS(e.cover.cssTempFile, defaultCoverCSSFilename)
		// If that doesn't work, generate a filename
		if _, ok := err.(*FilenameAlreadyUsedError); ok {
			coverCSSFilename := fmt.Sprintf(
				cssFileFormat,
				len(e.css)+1,
				".css",
			)

			internalCSSPath, err = e.addCSS(e.cover.cssTempFile, coverCSSFilename)
			if _, ok := err.(*FilenameAlreadyUsedError); ok {
				// This shouldn't cause an error
				return fmt.Errorf("Error adding default cover CSS file: %w", err)
			}
		}
		if err != nil {
			if _, ok := err.(*FilenameAlreadyUsedError); !ok {
				return err
			}
		}
	}
	e.cover.cssFilename = filepath.Base(internalCSSPath)

	coverBody := fmt.Sprintf(defaultCoverBody, internalImagePath)
	// Title won't be used since the cover won't be added to the TOC
	// First try to use the default cover filename
	coverPath, err := e.addSection("", coverBody, "", defaultCoverXhtmlFilename, internalCSSPath)
	// If that doesn't work, generate a filename
	if _, ok := err.(*FilenameAlreadyUsedError); ok {
		coverPath, err = e.addSection("", coverBody, "", "", internalCSSPath)
		if _, ok := err.(*FilenameAlreadyUsedError); ok {
			// This shouldn't cause an error since we're not specifying a filename
			return fmt.Errorf("Error adding default cover XHTML file: %w", err)
		}
	}
	e.cover.xhtmlFilename = filepath.Base(coverPath)
	return nil
}

// SetIdentifier sets the unique identifier of the EPUB, such as a UUID, DOI,
// ISBN or ISSN. If no identifier is set, a UUID will be automatically
// generated.
func (e *Epub) SetIdentifier(identifier string) {
	e.Lock()
	defer e.Unlock()
	e.identifier = identifier
	e.pkg.setIdentifier(identifier)
	e.toc.setIdentifier(identifier)
}

// SetLang sets the language of the EPUB.
func (e *Epub) SetLang(lang string) {
	e.Lock()
	defer e.Unlock()
	e.lang = lang
	e.pkg.setLang(lang)
}

// SetDescription sets the description of the EPUB.
func (e *Epub) SetDescription(desc string) {
	e.Lock()
	defer e.Unlock()
	e.desc = desc
	e.pkg.setDescription(desc)
}

// SetPpd sets the page progression direction of the EPUB.
func (e *Epub) SetPpd(direction string) {
	e.Lock()
	defer e.Unlock()
	e.ppd = direction
	e.pkg.setPpd(direction)
}

// SetTitle sets the title of the EPUB.
func (e *Epub) SetTitle(title string) {
	e.Lock()
	defer e.Unlock()
	e.title = title
	e.pkg.setTitle(title)
	e.toc.setTitle(title)
}

// Title returns the title of the EPUB.
func (e *Epub) Title() string {
	return e.title
}

// EmbedImages download <img> tags in EPUB and modify body to show images
// file inside of EPUB:
// ../ImageFolderName/internalFilename
//
// The image source should either be a URL, a path to a local file, or an embedded data URL; in any
// case, the image file will be retrieved and stored in the EPUB.
//
// The internal filename will be used when storing the image file in the EPUB
// and must be unique among all image files. If the same filename is used more
// than once, FilenameAlreadyUsedError will be returned. The internal filename is
// optional; if no filename is provided, one will be generated.
// if go-epub can't download image it keep it untoch and not return any error just log that

// Just call EmbedImages() after section added
func (e *Epub) EmbedImages() {
	imageTagRegex := regexp.MustCompile(`<img.*?src="(.*?)".*?>`)
	for i, section := range e.sections {
		imageTagMatches := imageTagRegex.FindAllStringSubmatch(section.xhtml.xml.Body.XML, -1)

		// Check if imageTagMatches is empty
		if len(imageTagMatches) == 0 {
			continue // Skip to the next section
		}
		images := make(map[string]string)

		for _, match := range imageTagMatches {
			imageURL := match[1]
			if !strings.HasPrefix(imageURL, "data:image/") {
				// Check if the image exists somewhere else in the document, to avoid processing it several times
				if _, exists := images[imageURL]; exists {
					continue
				}
				originalImgTag := match[0]
				images[imageURL] = match[1]

				// organize img tags first one always be src and other data-src
				// at least one src that point to the file inside epub
				// no dupolicate src that point to the same file
				// you can read more details https://github.com/go-shiori/go-epub/pull/3#issuecomment-1703777716
				// Replace all "data-src=" with "src="
				match[0] = strings.ReplaceAll(match[0], " data-src=", " src=")

				firstSrcIndex := strings.Index(match[0], " src=")
				match[0] = match[0][:firstSrcIndex+len(" src=")] + strings.ReplaceAll(match[0][firstSrcIndex+len(" src="):], " src=", " data-src=")
				parsedImageURL, err := url.Parse(imageURL)
				if err != nil {
					log.Printf("can't parse image URL: %s", err)
					continue
				}
				extension := filepath.Ext(parsedImageURL.Path)
				if extension == "" {
					res, err := http.Head(imageURL)
					if err != nil {
						log.Printf("can't get image headers: %s", err)
					} else {
						// Get extension from the file content type
						extensions, err := mime.ExtensionsByType(res.Header.Get("Content-Type"))
						if err != nil {
							log.Printf("can't get file type from content type: %s", err)
						} else if len(extensions) > 0 {
							extension = extensions[0]
						}
					}
				}
				filename := fmt.Sprintf("image%04d%s", len(e.images)+1, extension)
				filePath, err := e.AddImage(string(imageURL), filename)
				if err != nil {
					log.Printf("can't add image to the epub: %s", err)
					continue
				}
				newImgTag := strings.ReplaceAll(match[0], imageURL, filePath)
				e.sections[i].xhtml.xml.Body.XML = strings.ReplaceAll(section.xhtml.xml.Body.XML, originalImgTag, newImgTag)
			}
		}
	}
}

// Add a media file to the EPUB and return the path relative to the EPUB section
// files
func addMedia(client *http.Client, source string, internalFilename string, mediaFileFormat string, mediaFolderName string, mediaMap map[string]string) (string, error) {
	err := grabber{client}.checkMedia(source)
	if err != nil {
		return "", &FileRetrievalError{
			Source: source,
			Err:    err,
		}
	}
	if internalFilename == "" {
		// If a filename isn't provided, use the filename from the source
		internalFilename = filepath.Base(source)
		_, ok := mediaMap[internalFilename]
		// if filename is too long, invalid or already used, try to generate a unique filename
		if len(internalFilename) > 255 || !fs.ValidPath(internalFilename) || ok {
			internalFilename = fmt.Sprintf(
				mediaFileFormat,
				len(mediaMap)+1,
				strings.ToLower(filepath.Ext(source)),
			)
		}
	}

	if _, ok := mediaMap[internalFilename]; ok {
		return "", &FilenameAlreadyUsedError{Filename: internalFilename}
	}

	mediaMap[internalFilename] = source

	return path.Join(
		"..",
		mediaFolderName,
		internalFilename,
	), nil
}

// getFilenames returns a map of section filenames and index numbers within an ebook
func getFilenames(sections []*epubSection) map[string]int {
	filenames := make(map[string]int)
	index := 1

	for _, section := range sections {
		filenames[section.filename] = index
		index++

		if section.children != nil {
			childFilenames := getFilenames(section.children)
			for filename := range childFilenames {
				filenames[filename] = index
				index++
			}
		}
	}

	return filenames
}

// get filenamelist and return true if filename exist inside epub
func keyExists(m map[string]int, key string) bool {
	_, ok := m[key]
	return ok
}

// Find parent section and append epubSection to it
func sectionAppender(sections []*epubSection, parentFilename string, targetSection *epubSection) error {
	for _, section := range sections {
		if section.filename == parentFilename {
			section.children = append(section.children, targetSection)
			return nil
		}
		err := sectionAppender(section.children, parentFilename, targetSection)
		if err == nil {
			return nil
		}
	}

	return fmt.Errorf("parent section not found")
}
