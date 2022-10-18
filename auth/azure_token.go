package auth

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/equinor/flowify-workflows-server/user"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/golang-jwt/jwt/v4"
)

// implements user.User
type AzureTokenUser struct {
	Name  string      `json:"name"`
	Email string      `json:"email"`
	Oid   string      `json:"oid"`
	Roles []user.Role `json:"roles"`
	jwt.RegisteredClaims

	expectedAudience string
	expectedIssuer   string
}

func NewAzureTokenUser(audience string, issuer string) AzureTokenUser {
	// empty user
	user := AzureTokenUser{}

	// for validation
	user.expectedAudience = audience
	user.expectedIssuer = issuer

	return user
}

// The time to use when validating token life-time,
// defaults to time.Now which is UTC, https://tools.ietf.org/html/rfc7519#section-4.1.4
// can be temporarily overridden when testing
var TimeFunc = time.Now

// the same as the jwt KeyFunc
type AzureKeyFunc = func(claim *jwt.Token) (interface{}, error)

type AzureTokenAuthenticatorOptions struct {
	// Disable verification of the signature of the tokens, (claims are still validated)
	DisableVerification bool
}

type AzureTokenAuthenticator struct {
	KeyFunc AzureKeyFunc
	// the intended audience to be verified with the token `aud` claim
	Audience string
	// the issuer id to be verified with the token `iss` claim
	Issuer string

	// Use only in safe environments
	Options AzureTokenAuthenticatorOptions
}

func NewAzureTokenAuthenticator(KeyFunc AzureKeyFunc,
	Audience string,
	Issuer string,
	Options AzureTokenAuthenticatorOptions) AuthenticationClient {

	return AzureTokenAuthenticator{KeyFunc: KeyFunc,
		Audience: Audience, Issuer: Issuer,
		Options: Options}
}

func (a AzureTokenAuthenticator) Authenticate(r *http.Request) (user.User, error) {
	authStr := r.Header.Get("Authorization")

	// Permission injection is required
	if authStr == "" {
		return AzureTokenUser{}, fmt.Errorf("no Authorization header given")
	}

	parts := strings.SplitN(authStr, " ", 2)

	if len(parts) < 2 || !strings.EqualFold(parts[0], "bearer") {
		return AzureTokenUser{}, fmt.Errorf("bad Authorization header")
	}

	user := NewAzureTokenUser(a.Audience, a.Issuer)
	err := user.Parse(parts[1], a.KeyFunc, a.Options.DisableVerification)
	if err != nil {
		return AzureTokenUser{}, errors.Wrap(err, "authentication error")
	}
	return user, nil
}

func (t AzureTokenUser) GetUid() string        { return t.Oid }
func (t AzureTokenUser) GetName() string       { return t.Name }
func (t AzureTokenUser) GetEmail() string      { return t.Email }
func (t AzureTokenUser) GetRoles() []user.Role { return t.Roles }
func (t *AzureTokenUser) Parse(tokenString string, keyFunc AzureKeyFunc, disableVerification bool) error {
	if disableVerification {
		logrus.Warn("jwt token verification is DISABLED")
		if _, _, err := jwt.NewParser().ParseUnverified(tokenString, t); err != nil {
			return err
		}

		// parse unverified doesn't call validation, do it explicitly
		return t.Valid()
	}

	_, err := jwt.ParseWithClaims(tokenString, t, keyFunc)
	return err
}

// called from the jwt-parser code to ensure the token is valid wrt
// also called explicitly from the no-verification path of Parse
func (t AzureTokenUser) Valid() error {
	now := TimeFunc()

	requireSet := true
	// The claims below are optional, by default, but we force them tested

	if !t.VerifyExpiresAt(now, requireSet) {
		if t.ExpiresAt != nil {
			logrus.Warnf("token expired: 'now' > 'exp', %s < %s", now.UTC().Format(time.RFC3339), t.ExpiresAt.UTC().Format(time.RFC3339))
		} else {
			logrus.Warn("token missing 'exp' claim")
		}
		return fmt.Errorf("token expired")
	}

	if !t.VerifyIssuedAt(now, requireSet) {
		if t.IssuedAt != nil {
			logrus.Warnf("token used before issued: 'now' < 'iat', %s < %s", now.UTC().Format(time.RFC3339), t.IssuedAt.UTC().Format(time.RFC3339))
		} else {
			logrus.Warn("token missing 'iat' claim")
		}

		return fmt.Errorf("token not valid")
	}

	if !t.VerifyNotBefore(now, requireSet) {
		if t.NotBefore != nil {
			logrus.Warnf("token used before valid: 'now' < 'nbf' %s < %s", now.UTC().Format(time.RFC3339), t.NotBefore.UTC().Format(time.RFC3339))
		} else {
			logrus.Warn("token missing 'nbf' claim")
		}
		return fmt.Errorf("token not yet valid")
	}

	if !t.VerifyAudience(t.expectedAudience, requireSet) {
		if t.Audience != nil {
			logrus.Warnf("token bad aud claim (%s), expected %s", t.Audience, t.expectedAudience)
		} else {
			logrus.Warn("token missing 'aud' claim")
		}

		return fmt.Errorf("invalid token `aud`")
	}

	// dont mistake comparison semantics, 1 is *match*
	if subtle.ConstantTimeCompare([]byte(t.Issuer), []byte(t.expectedIssuer)) != 1 {
		logrus.Warnf("token bad iss claim (%s), expected: %s", t.Issuer, t.expectedIssuer)
		return fmt.Errorf("invalid token `iss`")
	}

	return nil
}

/*
func readAll(url string) ([]byte, error) {
	r, err := http.Get(url)
	if err != nil {
		return []byte{}, err
	}
	if r.StatusCode != http.StatusOK {
		return []byte{}, fmt.Errorf("could not get azure validation info")
	}

	buf := new(bytes.Buffer)
	if err := func() error { // scope for defer and err
		_, err := buf.ReadFrom(r.Body)
		defer r.Body.Close()
		return err
	}(); err != nil {
		return []byte{}, err
	}
	return buf.Bytes(), nil
}
*/
