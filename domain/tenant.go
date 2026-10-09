package domain

import (
	"database/sql/driver"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sirkostya009/barm"
)

// TenantID is a tenant's id, carried by every request in the X-Tenant-ID
// header. It reads and stores exactly like an ID, and is its own type so it
// cannot be mixed up with the id of a row.
type TenantID ID

func ParseTenantID(s string) (TenantID, error) {
	id, err := ParseID(s)
	return TenantID(id), err
}

func (id TenantID) String() string                      { return ID(id).String() }
func (id TenantID) AppendText(b []byte) ([]byte, error) { return ID(id).AppendText(b) }
func (id TenantID) MarshalText() ([]byte, error)        { return ID(id).MarshalText() }
func (id *TenantID) UnmarshalText(b []byte) error       { return (*ID)(id).UnmarshalText(b) }
func (id *TenantID) Scan(src any) error                 { return (*ID)(id).Scan(src) }
func (id TenantID) Value() (driver.Value, error)        { return ID(id).Value() }
func (id *TenantID) ScanUUID(v pgtype.UUID) error       { return (*ID)(id).ScanUUID(v) }
func (id TenantID) UUIDValue() (pgtype.UUID, error)     { return ID(id).UUIDValue() }

var ErrBadSlug = errors.New("slug must be 1-32 chars of [a-z0-9-]")

func validSlug(slug string) error {
	if len(slug) == 0 || len(slug) > 32 {
		return ErrBadSlug
	}
	for i := range len(slug) {
		c := slug[i]
		if c != '-' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return ErrBadSlug
		}
	}
	return nil
}

// Tenant lives in the shared schema, everything else lives in the tenant's own.
//
//ggen:generate
type Tenant struct {
	barm.BaseModel `barm:"table:shared.tenants,alias:t" json:"-"`

	ID        TenantID  `barm:"id,pk,default:uuidv7()" json:"id"`
	Slug      string    `barm:"slug" json:"slug"`
	Name      string    `barm:"name" json:"name"`
	CreatedAt time.Time `barm:"created_at,default:now()" json:"created_at"`
}

// CreateTenant is a Tenant without the columns the database fills in: barm
// inserts it and hands back the full row. It names no table, the repository
// that writes it does.
//
//ggen:generate
type CreateTenant struct {
	Slug string `barm:"slug" json:"slug" pipe:"required @validSlug"`
	Name string `barm:"name" json:"name" pipe:"required trim minrunes=1 maxrunes=64"`
}
