package httpguard

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	Off()
	os.Exit(m.Run())
}

func TestTheDefaultTransportRefuses(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	get := func(client *http.Client) error {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		res, err := client.Do(req)
		if res != nil {
			_ = res.Body.Close()
		}
		return err
	}
	if err := get(http.DefaultClient); !errors.Is(err, ErrSharedTransport) {
		t.Errorf("a request through the default = %v, want ErrSharedTransport", err)
	}
	if err := get(srv.Client()); err != nil {
		t.Errorf("a request through the server's own client: %v", err)
	}
}
