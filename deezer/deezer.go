package deezer

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

type Deezer struct {
	ctx           context.Context
	authState     string
	redirectURI   string
	authURL       *url.URL
	authSecretURL *url.URL
	oauth         *oauth2.Config
	TokenChannel  chan *oauth2.Token
	WG            *sync.WaitGroup
}

func NewDeezer(
	ctx context.Context,
	redirectURI string,
	wg *sync.WaitGroup,
	clientId string,
	secretKey string,
) *Deezer {
	/*authURL, _ := url.Parse("https://connect.deezer.com/oauth/auth.php")
	authURL.Query().Add("app_id", clientId)
	authURL.Query().Add("redirect_uri", redirectURI)
	authURL.Query().Add("perms", "email,offline_access")
	authURL.Query().Add("state", "abc123")

	authSecretURL, _ := url.Parse("https://connect.deezer.com/oauth/access_token.php")
	authSecretURL.Query().Add("app_id", clientId)
	authSecretURL.Query().Add("secret", secretKey)*/

	d := &Deezer{
		ctx:         ctx,
		authState:   "abc123",
		redirectURI: redirectURI,
		oauth: &oauth2.Config{
			ClientID:     clientId,
			RedirectURL:  redirectURI,
			ClientSecret: secretKey,
			Scopes:       []string{"email", "basic_access"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://connect.deezer.com/oauth/auth.php",
				TokenURL: "https://connect.deezer.com/oauth/access_token.php",
			},
		},
		TokenChannel: make(chan *oauth2.Token),
		WG:           wg,
	}
	d.httpServer()
	return d
}

func (s *Deezer) indexHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Got request for:", r.URL.String())
}
func (s *Deezer) callbackHandler(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	code := values.Get("code")
	if code == "" {
		log.Println("Error, no code provided (deezer)")
		return
	}

	if st := r.FormValue("state"); st != s.authState {
		http.NotFound(w, r)
		log.Fatalf("State mismatch: %s != %s\n", st, s.authState)
	}

	token, err := s.oauth.Exchange(s.ctx, code)
	if err != nil {
		log.Println("Error while retrieving token", err)
		return
	}

	log.Println("Received token on handler", token)
	s.TokenChannel <- token
}

func (s *Deezer) httpServer() {
	// first start an HTTP server
	router := http.NewServeMux()
	router.HandleFunc("/callback", s.callbackHandler)
	router.HandleFunc("/", s.indexHandler)

	// Done chan will be closed when server shutdown
	server := &http.Server{
		Addr:         ":8081",
		Handler:      router,
		ErrorLog:     &log.Logger{},
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	go func() {
		s.WG.Add(1)
		defer s.WG.Done()
		<-s.ctx.Done()
		ctxTimeout, cancelFn := context.WithTimeout(s.ctx, time.Second*5)
		defer cancelFn()
		log.Println("Closing auth http server (Deezer)...")
		close(s.TokenChannel)
		server.Shutdown(ctxTimeout)
	}()
	go func() {
		s.WG.Add(1)
		defer s.WG.Done()
		server.ListenAndServe()
		log.Println("Auth http server closed (Deezer)")
	}()
}

func (s *Deezer) GetDeezerUserClient() {

	url := s.oauth.AuthCodeURL(s.authState, oauth2.AccessTypeOffline)
	log.Println("Please log in to Deezer by visiting the following page in your browser:", url)

	return
}
