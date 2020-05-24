package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/easen/godeezer"
	_ "github.com/joho/godotenv/autoload"
	"github.com/zmb3/spotify"
)

// redirectURI is the OAuth redirect URI for the application.
// You must register an application at Spotify's developer portal
// and enter this value.
const redirectURI = "http://localhost:8080/callback"

var (
	spotifyAuthState    = "abc123"
	deezerAccessToken   = os.Getenv("DEEZER_ACCESS_TOKEN")
	spotifyTargetMarket = os.Getenv("SPOTIFY_TARGET_MARKET")
	stripBracketsRegexp = regexp.MustCompile(`(\(.*\))`)
)

func main() {
	spotifyClient := getSpotifyUserClient()
	// use the client to make calls that require authorization
	user, err := spotifyClient.CurrentUser()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("You are logged in as:", user.ID)

	syncArtists(spotifyClient)
	syncAlbums(spotifyClient)
	syncTracks(spotifyClient)
}

func syncTracks(spotifyClient *spotify.Client) {
	fmt.Println("Updating favourite tracks")
	deezerFavouriteTracks, err := godeezer.GetUserFavoriteTracks(deezerAccessToken)
	if err != nil {
		log.Fatal("godeezer.GetUserFavoriteTracks() fatal:", err)
		log.Fatal(err)
	}

	var spotifyIDs []spotify.ID
	var count = 0
	for i := 0; i < len(deezerFavouriteTracks); i++ {
		deezerTrack := deezerFavouriteTracks[i]
		var searchResult *spotify.SearchResult
		query := fmt.Sprintf("artist:\"%s\" track:\"%s\"", deezerTrack.Artist.Name, deezerTrack.Title)
		searchResult, _ = spotifyClient.Search(query, spotify.SearchTypeTrack)

		if searchResult == nil || len(searchResult.Tracks.Tracks) == 0 {
			searchResult, _ = spotifyClient.Search(deezerTrack.Title, spotify.SearchTypeTrack)
		}

		if searchResult == nil || len(searchResult.Tracks.Tracks) == 0 {
			deezerTrackNameWithNoBrackets := strings.Trim(stripBracketsRegexp.ReplaceAllString(deezerTrack.Title, ""), " ")
			searchResult, _ = spotifyClient.Search(deezerTrackNameWithNoBrackets, spotify.SearchTypeTrack)
		}

		spotifyTrack := trackConflictResolution(deezerTrack, searchResult.Tracks.Tracks)
		if spotifyTrack == nil {
			continue
		}
		spotifyIDs = append(spotifyIDs, spotifyTrack.ID)
		fmt.Printf("  %s - %s\n", spotifyTrack.Artists[0].Name, spotifyTrack.Name)
		if len(spotifyIDs) == 50 || i == len(deezerFavouriteTracks)-1 {
			err := spotifyClient.SaveTrack(spotifyIDs...)
			if err != nil {
				log.Fatal("spotifyClient.SaveTrack() fatal:", err)
			}
			count = count + len(spotifyIDs)
			spotifyIDs = nil
		}
	}

	fmt.Printf("Added %d tracks\n", count)
}

func syncAlbums(spotifyClient *spotify.Client) {
	fmt.Println("Update favourite albums")
	deezerFavouriteAlbums, err := godeezer.GetUserFavoriteAlbums(deezerAccessToken)
	if err != nil {
		log.Fatal("godeezer.GetUserFavoriteArtists() fatal:", err)
		log.Fatal(err)
	}

	var spotifyIDs []spotify.ID
	var count = 0
	for i := 0; i < len(deezerFavouriteAlbums); i++ {
		deezerAlbum := deezerFavouriteAlbums[i]

		var searchResult *spotify.SearchResult
		searchResult, _ = spotifyClient.Search(fmt.Sprintf("artist:\"%s\", album:\"%s\"", deezerAlbum.Artist.Name, deezerAlbum.Title), spotify.SearchTypeAlbum)

		if searchResult == nil || len(searchResult.Albums.Albums) == 0 {
			searchResult, _ = spotifyClient.Search(deezerAlbum.Title, spotify.SearchTypeAlbum)
		}

		if searchResult == nil || len(searchResult.Albums.Albums) == 0 {
			continue
		}
		spotifyAlbum := albumConflictResolution(deezerAlbum, searchResult.Albums.Albums)
		if spotifyAlbum == nil {
			continue
		}
		spotifyIDs = append(spotifyIDs, spotifyAlbum.ID)
		fmt.Printf("  %s - %s\n", spotifyAlbum.Artists[0].Name, spotifyAlbum.Name)
		if len(spotifyIDs) == 50 || i == len(deezerFavouriteAlbums)-1 {
			err := spotifyClient.SaveAlbum(spotifyIDs...)
			if err != nil {
				log.Fatal("spotifyClient.SaveAlbum() fatal:", err)
			}
			count = count + len(spotifyIDs)
			spotifyIDs = nil
		}
	}

	fmt.Printf("Added %d albums\n", count)
}

func trackConflictResolution(deezerTrack godeezer.Track, spotifyTracks []spotify.FullTrack) *spotify.FullTrack {
	if len(spotifyTracks) == 1 {
		return &spotifyTracks[0]
	}

	var targetMarket []spotify.FullTrack
	for _, spotifyTrack := range spotifyTracks {
		for _, market := range spotifyTrack.AvailableMarkets {
			if strings.EqualFold(spotifyTargetMarket, market) {
				targetMarket = append(targetMarket, spotifyTrack)
				break
			}
		}
	}
	if len(targetMarket) == 0 {
		return nil
	}

	deezerTrackNameWithNoBrackets := strings.Trim(stripBracketsRegexp.ReplaceAllString(deezerTrack.Title, ""), " ")

	var matchingName []spotify.FullTrack
	for _, spotifyTrack := range targetMarket {
		if strings.EqualFold(deezerTrack.Title, spotifyTrack.Name) || strings.EqualFold(deezerTrackNameWithNoBrackets, spotifyTrack.Name) {
			matchingName = append(matchingName, spotifyTrack)
		}
	}
	if len(matchingName) == 1 {
		return &matchingName[0]
	}

	var matchingDuration []spotify.FullTrack
	for _, spotifyTrack := range matchingName {
		if deezerTrack.Duration == spotifyTrack.Duration/1000 {
			matchingDuration = append(matchingDuration, spotifyTrack)
		}
	}
	if len(matchingDuration) == 1 {
		return &matchingDuration[0]
	}

	if len(matchingName) > 1 {
		return &matchingName[0]
	}

	return &targetMarket[0]
}

func albumConflictResolution(deezerAlbum godeezer.Album, spotifyAlbums []spotify.SimpleAlbum) *spotify.SimpleAlbum {
	if len(spotifyAlbums) == 1 {
		return &spotifyAlbums[0]
	}

	var targetMarket []spotify.SimpleAlbum
	for _, spotifyAlbum := range spotifyAlbums {
		for _, market := range spotifyAlbum.AvailableMarkets {
			if strings.EqualFold(spotifyTargetMarket, market) {
				targetMarket = append(targetMarket, spotifyAlbum)
				break
			}
		}
	}

	var matchingName []spotify.SimpleAlbum
	for _, spotifyAlbum := range targetMarket {
		if strings.EqualFold(deezerAlbum.Title, spotifyAlbum.Name) {
			matchingName = append(matchingName, spotifyAlbum)
		}
	}
	if len(matchingName) == 1 {
		return &matchingName[0]
	}

	var matchingReleaseDate []spotify.SimpleAlbum
	for _, spotifyAlbum := range matchingName {
		if deezerAlbum.ReleaseDateTime().Equal(spotifyAlbum.ReleaseDateTime()) {
			matchingReleaseDate = append(matchingReleaseDate, spotifyAlbum)
		}
	}
	if len(matchingReleaseDate) == 1 {
		return &matchingReleaseDate[0]
	}

	if len(matchingName) > 1 {
		return &matchingName[0]
	}

	return nil
}

func syncArtists(spotifyClient *spotify.Client) {
	fmt.Println("Update favourite artists")
	deezerFavouriteArtists, err := godeezer.GetUserFavoriteArtists(deezerAccessToken)
	if err != nil {
		log.Fatal("godeezer.GetUserFavoriteArtists() fatal:", err)
		log.Fatal(err)
	}

	var spotifyIDs []spotify.ID
	var count = 0
	for i := 0; i < len(deezerFavouriteArtists); i++ {
		item := deezerFavouriteArtists[i]
		searchResult, _ := spotifyClient.Search(item.Name, spotify.SearchTypeArtist)
		if searchResult == nil || searchResult.Artists.Total == 0 {
			continue
		}
		spotifyIDs = append(spotifyIDs, searchResult.Artists.Artists[0].ID)
		fmt.Printf("  %s\n", searchResult.Artists.Artists[0].Name)

		if len(spotifyIDs) == 50 || i == len(deezerFavouriteArtists)-1 {
			err := spotifyClient.FollowArtist(spotifyIDs...)
			if err != nil {
				log.Fatal("spotifyClient.FollowArtist() fatal:", err)
			}
			count = count + len(spotifyIDs)
			spotifyIDs = nil
		}
	}

	fmt.Printf("Added %d artists\n", count)
}

func getSpotifyUserClient() *spotify.Client {

	auth := spotify.NewAuthenticator(redirectURI, spotify.ScopeUserFollowModify, spotify.ScopeUserFollowRead, spotify.ScopeUserLibraryModify, spotify.ScopeUserLibraryRead)
	ch := make(chan *spotify.Client)

	// first start an HTTP server
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		tok, err := auth.Token(spotifyAuthState, r)
		if err != nil {
			http.Error(w, "Couldn't get token", http.StatusForbidden)
			log.Fatal(err)
		}
		if st := r.FormValue("state"); st != spotifyAuthState {
			http.NotFound(w, r)
			log.Fatalf("State mismatch: %s != %s\n", st, spotifyAuthState)
		}
		// use the token to get an authenticated client
		client := auth.NewClient(tok)
		fmt.Fprintf(w, "Login Completed!")
		ch <- &client
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Got request for:", r.URL.String())
	})
	go http.ListenAndServe(":8080", nil)

	url := auth.AuthURL(spotifyAuthState)
	fmt.Println("Please log in to Spotify by visiting the following page in your browser:", url)

	// wait for auth to complete
	client := <-ch

	return client
}
