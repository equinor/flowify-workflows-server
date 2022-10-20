package test

import (
	"encoding/json"
	"net/http"

	"github.com/equinor/flowify-workflows-server/user"
)

// specify an explicit type for inference
// eg ReadType[int](...)
func ReadType[T any](r *http.Response) (T, error) {
	bytes := ResponseBodyBytes(r)
	var item T

	err := json.Unmarshal(bytes, &item)
	return item, err
}

type SecretListing struct {
	Items []string
}

func ignore[T any](T) {}
func (s *e2eTestSuite) Test_SecretHandling_live_system() {

	type SecretField struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}

	type SecretFieldList struct {
		Items []SecretField `json:"items"`
	}

	require := s.Require()

	// 1. List secrets, empty?
	// 2. Add secret
	// 3. Get secret name
	const workspace string = "workspace/"
	{
		usr := user.MockUser{
			Name:  "F. Lowe",
			Email: "mail",
			Roles: []user.Role{user.Role("tester")},
		}
		s1 := SecretField{Key: "k1", Value: "v1"}
		{
			requestor := make_authenticated_requestor(s.client, usr)
			resp, err := requestor(server_addr+"/api/v1/secrets"+"/"+workspace, http.MethodGet, "")

			require.NoError(err, BodyStringer{resp.Body})

			require.Equal(http.StatusOK, resp.StatusCode, resp.Status)

			_, err = ReadType[SecretListing](resp)
			require.NoError(err)
		}

		{
			// try to add as user
			s1 := SecretField{Key: "k1", Value: "v1"}
			requestor := make_authenticated_requestor(s.client, usr)
			body, err := json.Marshal(s1)
			require.NoError(err)

			resp, err := requestor(server_addr+"/api/v1/secrets"+"/"+workspace+s1.Key, http.MethodPut, string(body))

			require.NoError(err, BodyStringer{resp.Body})
			require.Equal(http.StatusUnauthorized, resp.StatusCode, resp.Status)

		}
		admin := user.MockUser{
			Name:  "S. Wirlop",
			Email: "mail",
			Roles: []user.Role{user.Role("tester-admin")},
		}
		{
			// try to add as admin
			requestor := make_authenticated_requestor(s.client, admin)
			body, err := json.Marshal(s1)
			require.NoError(err)

			resp, err := requestor(server_addr+"/api/v1/secrets"+"/"+workspace+s1.Key, http.MethodPut, string(body))

			require.NoError(err, BodyStringer{resp.Body})
			require.Equal(http.StatusCreated, resp.StatusCode, BodyStringer{resp.Body})

		}

		{
			// try to delete key as user
			requestor := make_authenticated_requestor(s.client, usr)
			resp, err := requestor(server_addr+"/api/v1/secrets"+"/"+workspace+s1.Key, http.MethodDelete, "")

			require.NoError(err, BodyStringer{resp.Body})

			require.Equal(http.StatusUnauthorized, resp.StatusCode, resp.Status)
		}
		{
			// try to delete key as admin
			requestor := make_authenticated_requestor(s.client, admin)
			resp, err := requestor(server_addr+"/api/v1/secrets"+"/"+workspace+s1.Key, http.MethodDelete, "")

			require.NoError(err, BodyStringer{resp.Body})

			require.Equal(http.StatusNoContent, resp.StatusCode, resp.Status)
		}

	}
}
