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

package encryption

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

type EncryptionOptions struct {
	NoEncryption bool
	JustKey      bool
	Pass         string
	Filepath     string
}

func NewEncryptor(filepath, pass string, encrypted, justKey bool) *EncryptionOptions {
	return &EncryptionOptions{
		Filepath:     filepath,
		Pass:         pass,
		NoEncryption: encrypted,
		JustKey:      justKey,
	}
}

func (e *EncryptionOptions) GetWriters(file io.Writer) (*gzip.Writer, *tar.Writer, *cipher.StreamWriter, error) {
	var err error
	if e.NoEncryption {
		gzw, err := gzip.NewWriterLevel(file, gzip.BestCompression)
		if err != nil {
			return nil, nil, nil, errors.Wrap(err, "Failed to create gzip writer")
		}
		tw := tar.NewWriter(gzw)
		return gzw, tw, nil, nil // return file<-gzip<-tar
	}

	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, nil, nil, errors.Wrap(err, "Failed to generate salt")
	}
	if e.Pass == "" {
		log.Debug().Msg("Password not provided, generating new")
		err := e.generatePassword()
		if err != nil {
			return nil, nil, nil, errors.Wrap(err, "Failed to generate random password")
		}
	}

	pbkdf, err := pbkdf2.Key(sha256.New, e.Pass, salt, iteration, split)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "Failed to generate key from pass")
	}
	key := pbkdf[:32]
	iv := pbkdf[32:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "Failed to generate key")
	}

	stream := cipher.NewCTR(block, iv)
	writer := &cipher.StreamWriter{S: stream, W: file}

	e.writeSaltToFile(file, salt)

	gzw, err := gzip.NewWriterLevel(writer, gzip.BestCompression)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "Failed to create gzip writer")
	}
	tw := tar.NewWriter(gzw)

	return gzw, tw, writer, nil // return file<-encryption<-gzip<-tar
}

func (e *EncryptionOptions) GetReaders(r io.Reader) (*gzip.Reader, *tar.Reader, *cipher.StreamReader, error) {
	var err error
	if e.NoEncryption {
		gzr, err := gzip.NewReader(r)
		if err != nil {
			return nil, nil, nil, errors.Wrap(err, "failed to create gzip reader")
		}
		tr := tar.NewReader(gzr)
		return gzr, tr, nil, nil // return file->gzip->tar
	}

	salt := make([]byte, saltSize+8) //nolint:mnd
	_, err = r.Read(salt)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "Failed to get salt")
	}
	salt = salt[8:]

	if e.Pass == "" {
		return nil, nil, nil, errors.New("Password not provided, please provide password")
	}

	pbkdf, err := pbkdf2.Key(sha256.New, e.Pass, salt, iteration, split)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "Failed to generate key from pass")
	}
	key := pbkdf[:32]
	iv := pbkdf[32:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "Failed to generate key")
	}

	stream := cipher.NewCTR(block, iv)
	reader := &cipher.StreamReader{S: stream, R: r}

	gzr, err := gzip.NewReader(reader)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "failed to open as gzip")
	}

	tr := tar.NewReader(gzr)
	return gzr, tr, reader, nil // return file->decryption->gzip->tar
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
	if e.NoEncryption {
		return nil
	}

	if e.JustKey {
		wr := zerolog.ConsoleWriter{
			Out:     os.Stderr,
			NoColor: true,
		}
		wr.PartsOrder = []string{
			zerolog.MessageFieldName,
		}
		lo := log.Output(wr)
		lo.Info().Msg("Pass: " + e.Pass)
	} else {
		log.Info().Msg("Pass: " + e.Pass)
	}
	if e.Filepath != "" {
		log.Info().Msg("Exporting pass to file")
		file, err := os.Create(e.Filepath)
		if err != nil {
			return errors.Wrap(err, "failed to open pass file")
		}
		_, err = file.Write([]byte(e.Pass)) //nolint:mirror
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
	e.Pass = hex.EncodeToString(buffer)[:passwordSize]
	return nil
}
