package passkeys

import (
	"github.com/go-webauthn/webauthn/webauthn"
)

// passkeyUser implements webauthn.User for registration and login ceremonies.
type passkeyUser struct {
	id          []byte
	name        string
	credentials []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte                         { return u.id }
func (u *passkeyUser) WebAuthnName() string                       { return u.name }
func (u *passkeyUser) WebAuthnDisplayName() string                { return u.name }
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }
