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

type Encyptor struct {
	noEncryption bool
	justKey      bool
	pass         string
	filepath     string
}

func NewEncryptor(filepath, pass string, encrypted, justKey bool) *Encyptor {
	return &Encyptor{
		filepath:     filepath,
		pass:         pass,
		noEncryption: encrypted,
		justKey:      justKey,
	}
}

func (e *Encyptor) GetEncryptedWriter(w io.Writer) (*cipher.StreamWriter, error) {
	salt := make([]byte, 8) //nolint:mnd
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, errors.Wrap(err, "Failed to generate salt")
	}
	if e.pass == "" {
		err := e.generatePassword()
		if err != nil {
			return nil, errors.Wrap(err, "Failed to generate random password")
		}
	}
	pbkdf, err := pbkdf2.Key(sha256.New, e.pass, salt, 10000, 48) //nolint:mnd
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
	writer := &cipher.StreamWriter{S: stream, W: w}
	e.writeSaltToFile(w, salt)
	return writer, nil
}

func (e *Encyptor) GetDecryptionReader(r io.Reader) (*cipher.StreamReader, error) {
	salt := make([]byte, 16) //nolint:mnd
	_, err := r.Read(salt)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get salt")
	}
	salt = salt[8:]
	if e.pass == "" {
		return nil, errors.New("Password not provided, please provide password")
	}
	pbkdf, err := pbkdf2.Key(sha256.New, e.pass, salt, 10000, 48) //nolint:mnd
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
	reader := &cipher.StreamReader{S: stream, R: r}
	return reader, nil
}

func (e *Encyptor) writeSaltToFile(w io.Writer, salt []byte) {
	prefix := []byte("Salted__")
	prefix = append(prefix, salt...)
	_, err := w.Write(prefix)
	if err != nil {
		panic("Failed to write salt")
	}
}

func (e *Encyptor) OutputPass() error {
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
		file.Write([]byte(e.pass)) //nolint:errcheck, gosec, mirror

		defer file.Close() //nolint:errcheck
	}
	return nil
}

func (e *Encyptor) generatePassword() error {
	buffer := make([]byte, 16) //nolint:mnd
	_, err := rand.Read(buffer)
	if err != nil {
		return err
	}
	e.pass = hex.EncodeToString(buffer)[:16]
	return nil
}
