package auth

import (
	"fmt"
	"time"

	"github.com/MicahParks/keyfunc"
	"github.com/equinor/flowify-workflows-server/user"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type AuthConfig struct {
	Handler string `mapstructure:"handler"`
	// the config is polymorphic based on the handler string
	Config map[string]interface{} `mapstructure:"config"`
}

type AzureConfig struct {
	Issuer   string
	Audience string
	KeysUrl  string
}

func NewAuthClientFromConfig(config AuthConfig) (AuthClient, error) {

	switch config.Handler {
	case "azure-oauth2-openid-token":
		{
			var azData AzureConfig
			err := mapstructure.Decode(config.Config, &azData)
			if err != nil {
				return nil, errors.Wrapf(err, "could not decode AuthConfig: %v", config.Config)
			}

			opts := AzureTokenAuthenticatorOptions{}
			var jwks AzureKeyFunc
			if azData.KeysUrl == "DISABLE_JWT_SIGNATURE_VERIFICATION" {
				log.Warn("running the authenticator without signature verification is UNSAFE")
				opts.DisableVerification = true
			} else {
				// Create the JWKS from the resource at the given URL.
				JWKS, err := keyfunc.Get(azData.KeysUrl, keyfunc.Options{
					// best practices for azure key roll-over: https://docs.microsoft.com/en-us/azure/active-directory/develop/active-directory-signing-key-rollover
					RefreshInterval:  time.Hour * 24,
					RefreshRateLimit: time.Minute * 5,
					// when encountering a "new" key id, allow immediate refresh (rate limited)
					RefreshUnknownKID: true,
					// make sure errors make it into the log
					RefreshErrorHandler: func(err error) { log.Error("jwks refresh error:", err) },
				})
				if err != nil {
					return nil, errors.Wrap(err, "failed to get the JWKS")
				}
				jwks = JWKS.Keyfunc
			}
			return AzureTokenAuthenticator{Issuer: azData.Issuer, Audience: azData.Audience, KeyFunc: jwks, Options: opts}, nil
		}

	case "disabled-auth":
		{
			var muser user.MockUser
			err := mapstructure.Decode(config.Config, &muser)
			if err != nil {
				return nil, errors.Wrapf(err, "could not decode AuthConfig: %v", config.Config)
			}
			log.Warn("flowify using no authentication and static dummy-authorization: User = ", muser)

			return MockAuthenticator{
				User: muser,
			}, nil
		}
	default:
		{
			return nil, fmt.Errorf("auth handler (%s) not supported", config.Handler)
		}
	}
}
