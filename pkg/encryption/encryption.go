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
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	iteration          int = 10000 // Openssl makes this number of iterations by default when encrypting/decrypting with pbdkf2
	split              int = 48    // Number of bytes needed to get key and iv from pbkdf2. 32 on key and rest on iv
	saltSize           int = 8     // Salt size by default in openssl
	passwordSize       int = 16    // Size of password in bytes when generating random password
	filePassPermission     = 0o600
)

type Options struct {
	Encryption bool
	JustKey    bool
	Pass       string
	Filepath   string
	Force      bool
}

func (e *Options) NewWriter(file io.Writer) (*cipher.StreamWriter, error) {
	var err error
	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}
	if e.Pass == "" {
		log.Debug().Msg("Password not provided, generating new")
		err := e.generatePassword()
		if err != nil {
			return nil, fmt.Errorf("failed to generate random password: %w", err)
		}
	}

	pbkdf, err := pbkdf2.Key(sha256.New, e.Pass, salt, iteration, split)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key from pass: %w", err)
	}
	key := pbkdf[:32]
	iv := pbkdf[32:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to generate cipher: %w", err)
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
		return nil, fmt.Errorf("failed to get salt: %w", err)
	}
	salt = salt[8:]

	if e.Pass == "" {
		return nil, errors.New("password not provided, please provide password")
	}

	pbkdf, err := pbkdf2.Key(sha256.New, e.Pass, salt, iteration, split)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key from pass: %w", err)
	}
	key := pbkdf[:32]
	iv := pbkdf[32:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to generate cipher: %w", err)
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
	if e.Filepath != "" {
		log.Info().Msg("Exporting password to file " + e.Filepath)
		switch e.Force {
		case true:
			file, err := os.OpenFile(e.Filepath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, filePassPermission)
			if err != nil {
				return fmt.Errorf("failed to open password file: %w", err)
			}

			_, err = file.Write([]byte(e.Pass)) //nolint:mirror
			if err != nil {
				return fmt.Errorf("failed to write to file: %w", err)
			}
			defer file.Close() //nolint:errcheck

		case false:
			_, err := os.Stat(e.Filepath)
			if err == nil {
				return errors.New("file for exporting password exist: use flag --force-pass-filepath to overwrite file")
			}

			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("falied to get stats of password file: %w", err)
			}

			file, err := os.OpenFile(e.Filepath, os.O_CREATE|os.O_WRONLY, filePassPermission)
			if err != nil {
				return fmt.Errorf("failed to open password file: %w", err)
			}

			_, err = file.Write([]byte(e.Pass)) //nolint:mirror
			if err != nil {
				return fmt.Errorf("failed to write to file: %w", err)
			}
			defer file.Close() //nolint:errcheck
		}
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
		lo.Info().Msg("Password: " + e.Pass)
	} else {
		log.Info().Msg("Password: " + e.Pass)
	}
	return nil
}

func (e *Options) generatePassword() error {
	buffer := make([]byte, passwordSize)
	_, err := rand.Read(buffer)
	if err != nil {
		return err
	}
	e.Pass = hex.EncodeToString(buffer)[:passwordSize]
	return nil
}
