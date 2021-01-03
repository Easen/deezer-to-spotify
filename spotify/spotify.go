package spotify

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/zmb3/spotify"
)

type Client = spotify.Client
type Spotify struct {
	ctx           context.Context
	authState     string
	redirectURI   string
	authenticator spotify.Authenticator
	ClientChannel chan *spotify.Client
	WG            *sync.WaitGroup
}

func NewSpotify(
	ctx context.Context,
	redirectURI string,
	wg *sync.WaitGroup,
	clientId string,
	secretKey string,
) *Spotify {
	auth := spotify.NewAuthenticator(
		redirectURI,
		spotify.ScopePlaylistReadPrivate,
		spotify.ScopePlaylistModifyPrivate,
	)
	auth.SetAuthInfo(clientId, secretKey)
	spot := &Spotify{
		ctx:           ctx,
		authState:     "abc123",
		redirectURI:   redirectURI,
		authenticator: auth,
		ClientChannel: make(chan *spotify.Client),
		WG:            wg,
	}
	spot.httpServer()
	return spot
}

func (s *Spotify) indexHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Got request for:", r.URL.String())
}
func (s *Spotify) callbackHandler(w http.ResponseWriter, r *http.Request) {
	tok, err := s.authenticator.Token(s.authState, r)
	if err != nil {
		http.Error(w, "Couldn't get token", http.StatusForbidden)
		log.Fatal(err)
	}
	if st := r.FormValue("state"); st != s.authState {
		http.NotFound(w, r)
		log.Fatalf("State mismatch: %s != %s\n", st, s.authState)
	}
	// use the token to get an authenticated client
	client := s.authenticator.NewClient(tok)
	log.Println("Received token on handler", tok)
	client.AutoRetry = true
	s.ClientChannel <- &client
}

func (s *Spotify) httpServer() {
	// first start an HTTP server
	router := http.NewServeMux()
	router.HandleFunc("/callback", s.callbackHandler)
	router.HandleFunc("/", s.indexHandler)

	// Done chan will be closed when server shutdown
	server := &http.Server{
		Addr:         ":8080",
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
		log.Println("Closing auth http server (spotify)...")
		close(s.ClientChannel)
		server.Shutdown(ctxTimeout)
	}()
	go func() {
		s.WG.Add(1)
		defer s.WG.Done()
		server.ListenAndServe()
		log.Println("Auth http server closed (spotify)")
	}()
}

func (s *Spotify) GetSpotifyUserClient() {

	url := s.authenticator.AuthURL(s.authState)
	log.Println("Please log in to Spotify by visiting the following page in your browser:", url)

	return
}
