package toolkit

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const randomStringSource = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_+"

// Tools is the type used to instantiate this module. Any variable of this type will have access to all the methods with the receiver *Tools
type Tools struct {
	MaxFileSize int
	// AllowedFileTypes are the ONLY types of files that will be allowed to upload
	AllowedFileTypes   []string
	MaxJSONSize        int
	AllowUnknownFields bool
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

func (t *Tools) UploadOneFile(request *http.Request, uploadDir string, rename ...bool) (*UploadedFile, error) {
	renameFile := true
	if len(rename) > 0 {
		renameFile = rename[0]
	}
	files, err := t.UploadFiles(request, uploadDir, renameFile)
	if err != nil {
		return nil, err
	}

	return files[0], nil
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

	err := t.CreateDirIfNotExists(uploadDir)
	if err != nil {
		return nil, err
	}

	// Parse the multipart form data with a specified max file size
	err = r.ParseMultipartForm(int64(t.MaxFileSize))
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
				uploadSingleFile.OriginalFileName = hdr.Filename

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

// CreateDirIfNotExists creates a directory, and add all necessary parents, if it does not exist
func (t *Tools) CreateDirIfNotExists(path string) error {
	const mode = 0755
	if _, err := os.Stat(path); os.IsNotExist(err) {
		err := os.MkdirAll(path, mode)
		if err != nil {
			return err
		}
	}
	return nil
}

// Slugify is a simple mean of creating a slug from a string
func (t *Tools) Slugify(s string) (string, error) {
	if s == "" {
		return "", errors.New("empty string not permitted")
	}
	var regEx = regexp.MustCompile(`[^a-z\d]+`)
	slug := strings.Trim(regEx.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(slug) == 0 {
		return "", errors.New("after removing characters, slug is zero length")
	}
	return slug, nil
}

// DownloadStaticFile downloads a file, and tries to force the browser to avoid displaying it
// in the browser windows by setting content disposition. It also allows specification of the
// display name.
func (t *Tools) DownloadStaticFile(writer http.ResponseWriter, request *http.Request, pathName, displayName string) {
	writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", displayName))

	http.ServeFile(writer, request, pathName)
}

// JSONResponse is the type used for sending JSON around
type JSONResponse struct {
	Error   bool        `json:"error"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// ReadJSON tries to read the body of a request and converts from json into a go data variable
func (t *Tools) ReadJSON(writer http.ResponseWriter, request *http.Request, data interface{}) error {
	maxBytes := 1024 * 1024 // 1MB
	if t.MaxJSONSize != 0 {
		maxBytes = t.MaxJSONSize
	}
	request.Body = http.MaxBytesReader(writer, request.Body, int64(maxBytes))

	decode := json.NewDecoder(request.Body)

	if !t.AllowUnknownFields {
		decode.DisallowUnknownFields()
	}

	err := decode.Decode(data)
	if err != nil {
		var syntaxError *json.SyntaxError
		var unmarshalTypeError *json.UnmarshalTypeError
		var invalidUnmarshalError *json.InvalidUnmarshalError

		switch {
		case errors.As(err, &syntaxError):
			return fmt.Errorf("body contains badly-formed JSON (at character %d)", syntaxError.Offset)

		case errors.Is(err, io.ErrUnexpectedEOF):
			return errors.New("body contains badly-formed JSON")

		case errors.As(err, &unmarshalTypeError):
			if unmarshalTypeError.Field != "" {
				return fmt.Errorf("body contains incorrect JSON type for field %q", unmarshalTypeError.Field)
			}
			return fmt.Errorf("body contains incorrect JSON type (at character %d)", unmarshalTypeError.Offset)

		case errors.Is(err, io.EOF):
			return errors.New("body must not be empty")

		case strings.HasPrefix(err.Error(), "json: unknown field"):
			fieldName := strings.TrimPrefix(err.Error(), "json: unknown field")
			return fmt.Errorf("body contains unknown key %s", fieldName)

		case err.Error() == "http: request body too large":
			return fmt.Errorf("body must not be larger than %d", maxBytes)

		case errors.As(err, &invalidUnmarshalError):
			return fmt.Errorf("error unmarshalling JSON: %s", err.Error())

		default:
			return err
		}
	}

	err = decode.Decode(&struct{}{})
	if err != io.EOF {
		return errors.New("body must contain only one JSON value")
	}
	return nil
}

// WriteJSON takes a response status code and arbitrary data and writes json to the client
func (t *Tools) WriteJSON(writer http.ResponseWriter, status int, data interface{}, headers ...http.Header) error {
	out, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if len(headers) > 0 {
		for key, value := range headers[0] {
			writer.Header()[key] = value
		}
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, err = writer.Write(out)
	if err != nil {
		return err
	}
	return nil
}

// ErrorJSON takes an error, and optionally a status code, and generates and sends a JSON error message
func (t *Tools) ErrorJSON(writer http.ResponseWriter, err error, status ...int) error {
	statusCode := http.StatusBadRequest

	if len(status) > 0 {
		statusCode = status[0]
	}
	var payload JSONResponse
	payload.Error = true
	payload.Message = err.Error()

	return t.WriteJSON(writer, statusCode, payload)
}

// PushJSONToRemote arbitrary data to some URL as JSON, and returns the response, status code, and error, if any...
// The final parameter, client, is optional. If none is  specified, we use the standard http.Client.
func (t *Tools) PushJSONToRemote(uri string, data any, client ...*http.Client) (*http.Response, int, error) {
	// create json
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, 0, err
	}
	// check for custom http client
	httpClient := &http.Client{}
	if len(client) > 0 {
		httpClient = client[0]
	}
	// build the request and set the header
	request, err := http.NewRequest("POST", uri, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	// call the remote uri
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	// send response back
	return response, response.StatusCode, nil
}
