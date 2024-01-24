package toolkit

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const randomStringSource = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_+"

// Tools is the type used to instantiate this module. Any variable of this type will have access to all the methods with the receiver *Tools
type Tools struct {
	MaxFileSize int
	// AllowedFileTypes are the ONLY types of files that will be allowed to upload
	AllowedFileTypes []string
}

// RandomString Returns a string of random characters of length n, using randomStringSource as the source for the string
func (t *Tools) RandomString(n int) string {
	s, r := make([]rune, n), []rune(randomStringSource)
	for i := range s {
		p, _ := rand.Prime(rand.Reader, len(r))
		x, y := p.Uint64(), uint64(len(r))
		s[i] = r[x%y]
	}
	return string(s)
}

// UploadedFile is a struct used to save information about an uploaded file
type UploadedFile struct {
	NewFileName      string
	OriginalFileName string
	FileSize         int64
}

// UploadFiles handles the process of uploading files to the server
func (t *Tools) UploadFiles(r *http.Request, uploadDir string, rename ...bool) ([]*UploadedFile, error) {
	renameFile := true
	if len(rename) > 0 {
		renameFile = rename[0]
	}
	var uploadedFiles []*UploadedFile

	// Set a default MaxFileSize of 1GB if not provided
	if t.MaxFileSize == 0 {
		t.MaxFileSize = 1024 * 1024 * 1024
	}

	// Parse the multipart form data with a specified max file size
	err := r.ParseMultipartForm(int64(t.MaxFileSize))
	if err != nil {
		return nil, errors.New("the uploaded file is too big")
	}

	// Loop through each file header in the multipart form data
	for _, fHeaders := range r.MultipartForm.File {
		for _, hdr := range fHeaders {
			uploadedFiles, err = func(uploadedFiles []*UploadedFile) ([]*UploadedFile, error) {
				var uploadSingleFile UploadedFile

				// Open the uploaded file for reading
				infile, err := hdr.Open()
				if err != nil {
					return nil, err
				}
				defer infile.Close()

				// Read the first 512 bytes of the file to determine its type
				buff := make([]byte, 512)
				_, err = infile.Read(buff)
				if err != nil {
					return nil, err
				}

				// Check if the file type is allowed based on the provided AllowedFileTypes
				allowed := false
				fileType := http.DetectContentType(buff)

				if len(t.AllowedFileTypes) > 0 {
					for _, typeOfFile := range t.AllowedFileTypes {
						if strings.EqualFold(fileType, typeOfFile) {
							allowed = true
						}
					}
				} else {
					allowed = true
				}
				if !allowed {
					return nil, errors.New("the uploaded file type is not permitted")
				}

				// Seek back to the beginning of the file
				_, err = infile.Seek(0, 0)
				if err != nil {
					return nil, err
				}

				// Generate a new file name and determine the full path for saving
				if renameFile {
					uploadSingleFile.NewFileName = fmt.Sprintf("%s%s", t.RandomString(25), filepath.Ext(hdr.Filename))
				} else {
					uploadSingleFile.NewFileName = hdr.Filename
				}

				// Create the new file in the specified upload directory
				var outfile *os.File
				defer outfile.Close()

				if outfile, err = os.Create(filepath.Join(uploadDir, uploadSingleFile.NewFileName)); err != nil {
					return nil, err
				} else {
					// Copy the file content to the newly created file and record the file size
					fileSize, err := io.Copy(outfile, infile)
					if err != nil {
						return nil, err
					}
					uploadSingleFile.FileSize = fileSize
				}

				// Append the information of the uploaded file to the list of uploaded files
				uploadedFiles = append(uploadedFiles, &uploadSingleFile)
				return uploadedFiles, nil
			}(uploadedFiles)
			if err != nil {
				return uploadedFiles, err
			}
		}
	}
	return uploadedFiles, nil
}
