package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"

	"github.com/easen/deezer-to-spotify/deezer"
	"github.com/easen/deezer-to-spotify/deezerToSpotify"
	"github.com/easen/deezer-to-spotify/spotify"
	_ "github.com/joho/godotenv/autoload"
)

func main() {
	// Context
	ctx, cancelFn := context.WithCancel(context.Background())

	// Quit gracefully
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	wg := sync.WaitGroup{}

	go func() {
		wg.Add(1)
		defer wg.Done()
		<-quit
		log.Println("Exit...")
		cancelFn()
	}()

	// Deezer
	d := deezer.NewDeezer(
		ctx,
		"http://localhost:8081/callback",
		&wg,
		os.Getenv("DEEZER_CLIENT_ID"),
		os.Getenv("DEEZER_SECRET_KEY"),
	)
	d.GetDeezerUserClient()
	// wait for auth to complete
	oauthToken, ok := <-d.TokenChannel
	var deezerAccessToken string
	if ok && oauthToken != nil {
		deezerAccessToken = oauthToken.AccessToken
		log.Println("You are logged on deezer with access token :", deezerAccessToken)
	}

	// Spotify
	spot := spotify.NewSpotify(
		ctx,
		"http://localhost:8080/callback",
		&wg,
		os.Getenv("SPOTIFY_CLIENT_ID"),
		os.Getenv("SPOTIFY_SECRET_KEY"),
	)

	spot.GetSpotifyUserClient()

	// wait for auth to complete
	spotifyClient, ok := <-spot.ClientChannel

	if ok {
		user, err := spotifyClient.CurrentUser()
		if err != nil {
			log.Fatal(err)
		}
		log.Println("You are logged on spotify as :", user.ID)
	}

	syncTrack := deezerToSpotify.NewSyncTrack(spotifyClient, deezerAccessToken, ctx)
	syncTrack.SyncFavorites()
	// Wait for quit
	close(quit)
	wg.Wait()
	log.Println("Done.")

	/* Old code
	syncArtists(spotifyClient)
	syncAlbums(spotifyClient)
	syncTracks(spotifyClient)*/
}
