// Copyright 2023 Percona LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package transferer

import (
	"archive/tar"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	iteration    int = 10000 // Openssl makes this number of iterations by default when encrypting/decrypting with pbdkf2
	split        int = 48    // Number of bytes needed to get key and iv from pbkdf2. 32 on key and rest on iv
	saltSize     int = 8     // Salt size by default in openssl
	passwordSize int = 16    // Size of password in bytes when generating random password
)

var gzw *gzip.Writer
var tw *tar.Writer
var writer *cipher.StreamWriter
var gzr *gzip.Reader
var tr *tar.Reader
var reader *cipher.StreamReader

type EncryptionOptions struct {
	noEncryption bool
	justKey      bool
	pass         string
	filepath     string
}

func NewEncryptor(filepath, pass string, encrypted, justKey bool) *EncryptionOptions {
	return &EncryptionOptions{
		filepath:     filepath,
		pass:         pass,
		noEncryption: encrypted,
		justKey:      justKey,
	}
}

func (e *EncryptionOptions) GetWriter(w io.Writer) (*tar.Writer, error) {
	var err error
	if e.noEncryption {
		log.Debug().Msg("Creating writer without encryption")
		gzw, err = gzip.NewWriterLevel(w, gzip.BestCompression)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to create gzip writer")
		}

		tw = tar.NewWriter(gzw)
		return tw, nil // return file<-gzip<-tar
	}
	log.Debug().Msg("Creating writer with encryption")

	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, errors.Wrap(err, "Failed to generate salt")
	}
	if e.pass == "" {
		log.Debug().Msg("Password not provided, generating new")
		err := e.generatePassword()
		if err != nil {
			return nil, errors.Wrap(err, "Failed to generate random password")
		}
	}

	pbkdf, err := pbkdf2.Key(sha256.New, e.pass, salt, iteration, split)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to generate key from pass")
	}
	key := pbkdf[:32]
	iv := pbkdf[32:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to generate key")
	}

	stream := cipher.NewCTR(block, iv)
	writer = &cipher.StreamWriter{S: stream, W: w}

	e.writeSaltToFile(w, salt)

	gzw, err = gzip.NewWriterLevel(writer, gzip.BestCompression)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to create gzip writer")
	}
	tw = tar.NewWriter(gzw)

	return tw, nil // return file<-encryption<-gzip<-tar
}

func (e *EncryptionOptions) GetReader(r io.Reader) (*tar.Reader, error) {
	var err error
	if e.noEncryption {
		gzr, err = gzip.NewReader(r)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create gzip reader")
		}
		tr = tar.NewReader(gzr)
		return tr, nil // return file->gzip->tar
	}

	salt := make([]byte, saltSize+8) //nolint:mnd
	_, err = r.Read(salt)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get salt")
	}
	salt = salt[8:]

	if e.pass == "" {
		return nil, errors.New("Password not provided, please provide password")
	}

	pbkdf, err := pbkdf2.Key(sha256.New, e.pass, salt, iteration, split)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to generate key from pass")
	}
	key := pbkdf[:32]
	iv := pbkdf[32:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to generate key")
	}

	stream := cipher.NewCTR(block, iv)
	reader = &cipher.StreamReader{S: stream, R: r}

	gzr, err = gzip.NewReader(reader)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open as gzip")
	}

	tr = tar.NewReader(gzr)
	return tr, nil // return file->decryption->gzip->tar
}

func (e *EncryptionOptions) writeSaltToFile(w io.Writer, salt []byte) {
	prefix := []byte("Salted__")
	prefix = append(prefix, salt...)
	_, err := w.Write(prefix)
	if err != nil {
		panic("Failed to write salt")
	}
}

func (e *EncryptionOptions) OutputPass() error {
	if e.noEncryption {
		return nil
	}

	if e.justKey {
		wr := zerolog.ConsoleWriter{
			Out:     os.Stderr,
			NoColor: true,
		}
		wr.PartsOrder = []string{
			zerolog.MessageFieldName,
		}
		lo := log.Output(wr)
		lo.Info().Msg("Pass: " + e.pass)
	} else {
		log.Info().Msg("Pass: " + e.pass)
	}
	if e.filepath != "" {
		log.Info().Msg("Exporting pass to file")
		file, err := os.Create(e.filepath)
		if err != nil {
			return errors.Wrap(err, "failed to open pass file")
		}
		_, err = file.Write([]byte(e.pass))
		if err != nil {
			return errors.Wrap(err, "failed to write to file")
		}
		defer file.Close() //nolint:errcheck
	}
	return nil
}

func (e *EncryptionOptions) generatePassword() error {
	buffer := make([]byte, passwordSize)
	_, err := rand.Read(buffer)
	if err != nil {
		return err
	}
	e.pass = hex.EncodeToString(buffer)[:passwordSize]
	return nil
}

func (e *EncryptionOptions) closeWriters() {
	tw.Close()
	gzw.Close()
	if !e.noEncryption{
		writer.Close()
	}
}
func (e *EncryptionOptions) closeReaders() {
	gzr.Close()
}
