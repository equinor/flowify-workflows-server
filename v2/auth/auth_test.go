package auth

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
)

func makeToken(claims jwt.RegisteredClaims, signKey []byte, t *testing.T) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	ss, err := token.SignedString(signKey)
	require.Nil(t, err)
	return ss
}

func Test_TokenValidation(t *testing.T) {

	type testCase struct {
		Name          string
		Token         []byte
		Key           []byte
		ExpectedError error
	}
	testCases := []testCase{
		{
			Name: "passing test",
			Token: []byte(makeToken(jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Minute)),
				Issuer:    "test-issuer",
				Audience:  []string{"test-audience"},
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				NotBefore: jwt.NewNumericDate(time.Now()),
			}, []byte("secret"), t)),
		},
		{
			Name: "wrong issuer",
			Token: []byte(makeToken(jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Minute)),
				Issuer:    "bad-issuer",
				Audience:  []string{"test-audience"},
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				NotBefore: jwt.NewNumericDate(time.Now()),
			}, []byte("secret"), t)),
			ExpectedError: fmt.Errorf("invalid token `iss`"),
		},
		{
			Name: "missing issuer claim",
			Token: []byte(makeToken(jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Minute)),
				Audience:  []string{"test-audience"},
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				NotBefore: jwt.NewNumericDate(time.Now()),
			}, []byte("secret"), t)),
			ExpectedError: fmt.Errorf("invalid token `iss`"),
		},
		{
			Name: "wrong aud",
			Token: []byte(makeToken(jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Minute)),
				Issuer:    "test-issuer",
				Audience:  []string{"bad-audience", "rotten-audience"},
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				NotBefore: jwt.NewNumericDate(time.Now()),
			}, []byte("secret"), t)),
			ExpectedError: fmt.Errorf("invalid token `aud`"),
		},
		{
			Name: "mixed aud passes",
			Token: []byte(makeToken(jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Minute)),
				Issuer:    "test-issuer",
				Audience:  []string{"bad-audience", "rotten-audience", "test-audience"},
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				NotBefore: jwt.NewNumericDate(time.Now()),
			}, []byte("secret"), t)),
		},
		{
			Name: "missing aud claim",
			Token: []byte(makeToken(jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Minute)),
				Issuer:    "test-issuer",
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				NotBefore: jwt.NewNumericDate(time.Now()),
			}, []byte("secret"), t)),
			ExpectedError: fmt.Errorf("invalid token `aud`"),
		},
		{
			Name: "expired token",
			Token: []byte(makeToken(jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Minute)),
				Issuer:    "test-issuer",
				Audience:  []string{"test-audience"},
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				NotBefore: jwt.NewNumericDate(time.Now()),
			}, []byte("secret"), t)),
			ExpectedError: fmt.Errorf("token expired"),
		},
		{
			Name: "token not yet valid",
			Token: []byte(makeToken(jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Minute)),
				Issuer:    "test-issuer",
				Audience:  []string{"test-audience"},
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				NotBefore: jwt.NewNumericDate(time.Now().Add(5 * time.Second)),
			}, []byte("secret"), t)),
			ExpectedError: fmt.Errorf("token not yet valid"),
		},
		{
			Name: "token not valid",
			Token: []byte(makeToken(jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Minute)),
				Issuer:    "test-issuer",
				Audience:  []string{"test-audience"},
				IssuedAt:  jwt.NewNumericDate(time.Now().Add(2 * time.Minute)),
				NotBefore: jwt.NewNumericDate(time.Now()),
			}, []byte("secret"), t)),
			ExpectedError: fmt.Errorf("token not valid"),
		},
	}

	aud := "test-audience"
	iss := "test-issuer"

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			user := NewAzureTokenUser(aud, iss)
			err := user.Parse(string(test.Token), nil, true)
			require.Equal(t, test.ExpectedError, err)

		})
	}

	t.Run("TimeFunc meddling", func(t *testing.T) {
		user := NewAzureTokenUser(aud, iss)
		test := testCases[0]

		// make sure auth.TimeFunc is restored at scope end
		defer func(stash func() time.Time) { TimeFunc = stash }(TimeFunc)

		// time is offset into future, should fail
		TimeFunc = func() time.Time { return time.Now().Add(2 * time.Minute) }
		err := user.Parse(string(test.Token), nil, true)
		require.Equal(t, fmt.Errorf("token expired"), err)

		// reset timer, should pass
		TimeFunc = time.Now
		err = user.Parse(string(test.Token), nil, true)
		require.Equal(t, nil, err)

	})

	t.Run("make sure verification doesnt offset validation", func(t *testing.T) {
		{
			user := NewAzureTokenUser(aud, iss)
			test := testCases[0]

			err := user.Parse(string(test.Token), func(*jwt.Token) (interface{}, error) { return []byte("secret"), nil }, false)
			require.Equal(t, test.ExpectedError, err)
		}

		{
			user := NewAzureTokenUser(aud, iss)
			test := testCases[1]

			err := user.Parse(string(test.Token), func(*jwt.Token) (interface{}, error) { return []byte("secret"), nil }, false)
			require.Equal(t, test.ExpectedError, errors.Unwrap(err))
		}

	})

}
