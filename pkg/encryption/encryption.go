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

type Options struct {
	Encryption bool
	JustKey      bool
	Pass         string
	Filepath     string
}

func (e *Options) NewWriter(file io.Writer) (*cipher.StreamWriter, error) {
	var err error
	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, errors.Wrap(err, "Failed to generate salt")
	}
	if e.Pass == "" {
		log.Debug().Msg("Password not provided, generating new")
		err := e.generatePassword()
		if err != nil {
			return nil, errors.Wrap(err, "Failed to generate random password")
		}
	}

	pbkdf, err := pbkdf2.Key(sha256.New, e.Pass, salt, iteration, split)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to generate key from pass")
	}
	key := pbkdf[:32]
	iv := pbkdf[32:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to generate cipher")
	}

	stream := cipher.NewCTR(block, iv)
	writer := &cipher.StreamWriter{S: stream, W: file}

	e.writeSaltToFile(file, salt)

	return writer, nil
}

func (e *Options) GetReader(r io.Reader) (*cipher.StreamReader, error) {
	var err error
	salt := make([]byte, saltSize+8) //nolint:mnd
	_, err = r.Read(salt)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get salt")
	}
	salt = salt[8:]

	if e.Pass == "" {
		return nil, errors.New("Password not provided, please provide password")
	}

	pbkdf, err := pbkdf2.Key(sha256.New, e.Pass, salt, iteration, split)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to generate key from pass")
	}
	key := pbkdf[:32]
	iv := pbkdf[32:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to generate cipher")
	}

	stream := cipher.NewCTR(block, iv)
	reader := &cipher.StreamReader{S: stream, R: r}

	return reader, nil
}

func (e *Options) writeSaltToFile(w io.Writer, salt []byte) {
	prefix := []byte("Salted__")
	prefix = append(prefix, salt...)
	_, err := w.Write(prefix)
	if err != nil {
		panic("Failed to write salt")
	}
}

func (e *Options) OutputPass() error {
	if !e.Encryption {
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
			return errors.Wrap(err, "failed to open password file")
		}
		_, err = file.Write([]byte(e.Pass)) //nolint:mirror
		if err != nil {
			return errors.Wrap(err, "failed to write to file")
		}
		defer file.Close() //nolint:errcheck
	}
	return nil
}

func (e *Options) generatePassword() error {
	buffer := make([]byte, passwordSize)
	_, err := rand.Read(buffer)
	if err != nil {
		return err
	}
	log.Debug().Msg(string(buffer))
	e.Pass = hex.EncodeToString(buffer)[:passwordSize]
	log.Debug().Msg(e.Pass)
	return nil
}
