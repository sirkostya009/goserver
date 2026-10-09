package domain

import (
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"fmt"
	"unsafe"
	"uuid"

	"github.com/jackc/pgx/v5/pgtype"
)

// ID is the id of a row: a uuid, v7 when the database generated it.
//
// As text, on the wire, it is unpadded base64url: 22 characters against the 36
// of the RFC form. In the database it is a plain uuid.
type ID uuid.UUID

const idTextLen = 22

var ErrBadID = errors.New("id must be 22 chars of base64url")

// Strict rejects ids that set the 2 spare bits of the last symbol, so every id
// has exactly one spelling.
var idEncoding = base64.RawURLEncoding.Strict()

func ParseID(s string) (ID, error) {
	var id ID
	if len(s) != idTextLen {
		return id, ErrBadID
	}
	//nolint:gosec // read-only zero-copy view of s, never retained past this call
	if _, err := idEncoding.Decode(id[:], unsafe.Slice(unsafe.StringData(s), len(s))); err != nil {
		return ID{}, ErrBadID
	}
	return id, nil
}

func (id ID) String() string {
	var b [idTextLen]byte
	idEncoding.Encode(b[:], id[:])
	return string(b[:])
}

func (id ID) AppendText(b []byte) ([]byte, error) {
	return idEncoding.AppendEncode(b, id[:]), nil
}

func (id ID) MarshalText() ([]byte, error) {
	return id.AppendText(make([]byte, 0, idTextLen))
}

func (id *ID) UnmarshalText(b []byte) error {
	//nolint:gosec // read-only zero-copy view of b, never retained past this call
	parsed, err := ParseID(unsafe.String(unsafe.SliceData(b), len(b)))
	if err == nil {
		*id = parsed
	}
	return err
}

// ScanUUID is how pgx hands over a uuid: the 16 bytes it read, where Scan
// would get them formatted as text only to parse them back.
func (id *ID) ScanUUID(v pgtype.UUID) error {
	if !v.Valid {
		return errors.New("domain: cannot scan NULL into an id")
	}
	*id = v.Bytes
	return nil
}

// UUIDValue is ScanUUID's counterpart for arguments.
func (id ID) UUIDValue() (pgtype.UUID, error) {
	return pgtype.UUID{Bytes: id, Valid: true}, nil
}

// Scan reads a postgres uuid, which a database/sql driver hands over in its RFC
// form.
func (id *ID) Scan(src any) error {
	var u uuid.UUID
	var err error
	switch v := src.(type) {
	case string:
		u, err = uuid.Parse(v)
	case []byte:
		if len(v) == len(u) {
			copy(u[:], v)
		} else {
			err = u.UnmarshalText(v)
		}
	case [16]byte:
		u = v
	default:
		err = fmt.Errorf("domain: cannot scan %T into an id", src)
	}
	*id = ID(u)
	return err
}

func (id ID) Value() (driver.Value, error) {
	return uuid.UUID(id).String(), nil
}
