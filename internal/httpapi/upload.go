package httpapi

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
)

const multipartOverhead = 1 << 20

// withUploadedFile streams the "file" part of a multipart request to
// consume without buffering it in memory or in a system temp directory.
func withUploadedFile(w http.ResponseWriter, r *http.Request, maxBytes int64, consume func(name string, body io.Reader) error) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+multipartOverhead)
	mr, err := r.MultipartReader()
	if err != nil {
		return errBadRequest("formulaire multipart attendu")
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return errBadRequest("fichier manquant")
		}
		if err != nil {
			return err
		}
		if part.FormName() != "file" {
			part.Close()
			continue
		}
		name := path.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
		err = consume(name, part)
		part.Close()
		return err
	}
}

// saveUploadToTemp stores the uploaded file in dir and returns its path.
func saveUploadToTemp(w http.ResponseWriter, r *http.Request, dir string, maxBytes int64) (string, error) {
	tmp, err := os.CreateTemp(dir, "upload-*")
	if err != nil {
		return "", err
	}
	err = withUploadedFile(w, r, maxBytes, func(_ string, body io.Reader) error {
		_, err := io.Copy(tmp, body)
		return err
	})
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}
