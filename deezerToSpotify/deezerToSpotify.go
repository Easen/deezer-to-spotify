package deezerToSpotify

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/easen/godeezer"
	"github.com/go-redis/redis/v8"
	_ "github.com/joho/godotenv/autoload"
	"github.com/zmb3/spotify"
)

var (
	spotifyTargetMarket = os.Getenv("SPOTIFY_TARGET_MARKET")
	stripBracketsRegexp = regexp.MustCompile(`(\(.*\))`)
	useRedis            = true
)

type SyncTrack struct {
	rdb               *redis.Client
	spotifyClient     *spotify.Client
	deezerAccessToken string
	ctx               context.Context
}

type Track struct {
	DeezerId        string
	SpotifyId       string
	SpotifyTitle    string
	SpotifyArtist   string
	SpotifyDuration string
}

func NewSyncTrack(spotifyClient *spotify.Client, deezerAccessToken string, ctx context.Context) *SyncTrack {
	var rdb *redis.Client
	if useRedis {
		rdb = redis.NewClient(&redis.Options{
			Addr:     "localhost:6379",
			Password: "",
			DB:       0,
		})
	}
	return &SyncTrack{
		rdb:               rdb,
		ctx:               ctx,
		spotifyClient:     spotifyClient,
		deezerAccessToken: deezerAccessToken,
	}
}

func (s *SyncTrack) SyncFavorites() {
	deezerFavouriteTracks, err := godeezer.GetUserFavoriteTracks(s.deezerAccessToken)
	if err != nil {
		log.Fatal("godeezer.GetUserFavoriteTracks() fatal:", err)
		log.Fatal(err)
	}

	var spotifyIDs []spotify.ID
	var count = 0
	user, err := s.spotifyClient.CurrentUser()
	if err != nil {
		log.Fatal("Error getting spotify user id", err)
		return
	}
	playlist, err := s.spotifyClient.CreatePlaylistForUser(user.ID, fmt.Sprintf("Import depuis Deezer (%s)", time.Now().Format("02/01/2006 15:04:05")), "Import depuis Deezer", false)
	if err != nil {
		log.Fatal("Error create playlist", err)
		return
	}
	logFilename := fmt.Sprintf("Import %s", time.Now().Format("2006-01-02.15-04-05.txt"))
	logFile, _ := os.Create(logFilename)
	defer logFile.Close()

	for i := 0; i < len(deezerFavouriteTracks); i++ {
		select {
		case <-s.ctx.Done():
			return
		default:
			deezerTrack := deezerFavouriteTracks[i]
			deezerName := fmt.Sprintf("Deezer : %s - %s - %s", deezerTrack.Artist.Name, deezerTrack.Title, secondsToMinutes(deezerTrack.Duration))

			spotifyTrack, err := s.lookDeezerTrackOnSpotify(deezerTrack)
			if err != nil {
				l := fmt.Sprintf("%s | Spotify : %s\n",
					deezerName,
					err.Error(),
				)
				log.Print(l)
				logFile.WriteString(l)
				continue
			}
			spotifyIDs = append(spotifyIDs, spotify.ID(spotifyTrack.SpotifyId))
			l := fmt.Sprintf("%s | Spotify : %s - %s - %s\n",
				deezerName,
				spotifyTrack.SpotifyArtist, spotifyTrack.SpotifyTitle, spotifyTrack.SpotifyDuration,
			)
			log.Print(l)
			logFile.WriteString(l)

			if len(spotifyIDs) == 100 || i == len(deezerFavouriteTracks)-1 {

				_, err := s.spotifyClient.AddTracksToPlaylist(playlist.ID, spotifyIDs...)
				if err != nil {
					log.Fatal("spotifyClient.SaveTrack() fatal:", err)
				}

				count = count + len(spotifyIDs)
				spotifyIDs = nil
			}
		}
	}

	fmt.Printf("Added %d tracks\n", count)
}

func (s *SyncTrack) lookDeezerTrackOnSpotify(deezerTrack godeezer.Track) (*Track, error) {
	deezerId := strconv.Itoa(deezerTrack.ID)

	var cachedTrack Track
	if useRedis && s.rdb != nil {
		res := s.rdb.HGetAll(s.ctx, deezerId)
		if res.Err() == nil {
			t := res.Val()
			if _, ok := t["spotifyId"]; ok {
				log.Printf("Retrieved from cache : %s\n", deezerTrack.Title)
				t := res.Val()
				cachedTrack = Track{
					DeezerId:        deezerId,
					SpotifyId:       t["spotifyId"],
					SpotifyTitle:    t["spotifyTitle"],
					SpotifyArtist:   t["spotifyArtist"],
					SpotifyDuration: t["spotifyDuration"],
				}
				return &cachedTrack, nil
			}
		} else if res.Err() != redis.Nil {
			log.Printf("Error while retrieving from cache : %s\n", res.Err())
		}
	}

	var searchResult *spotify.SearchResult
	query := fmt.Sprintf("artist:\"%s\" track:\"%s\"", deezerTrack.Artist.Name, deezerTrack.Title)
	var err error
	searchResult, err = s.spotifyClient.SearchOpt(query, spotify.SearchTypeTrack, &spotify.Options{Country: &spotifyTargetMarket})
	if err != nil {
		return nil, fmt.Errorf("Error while search : %s", err)
	}

	if searchResult == nil || len(searchResult.Tracks.Tracks) == 0 {
		searchResult, err = s.spotifyClient.SearchOpt(deezerTrack.Title, spotify.SearchTypeTrack, &spotify.Options{Country: &spotifyTargetMarket})
		if err != nil {
			return nil, fmt.Errorf("Error while search : %s", err)
		}
	}

	if searchResult == nil || len(searchResult.Tracks.Tracks) == 0 {
		deezerTrackNameWithNoBrackets := strings.Trim(stripBracketsRegexp.ReplaceAllString(deezerTrack.Title, ""), " ")
		searchResult, err = s.spotifyClient.SearchOpt(deezerTrackNameWithNoBrackets, spotify.SearchTypeTrack, &spotify.Options{Country: &spotifyTargetMarket})
		if err != nil {
			return nil, fmt.Errorf("Error while search : %s", err)
		}
	}
	if searchResult == nil || len(searchResult.Tracks.Tracks) == 0 {
		return nil, fmt.Errorf("No result found")
	}

	spotifyTrack := trackConflictResolution(deezerTrack, searchResult.Tracks.Tracks)
	if spotifyTrack == nil {
		return nil, fmt.Errorf("Internal error")
	}
	track := Track{
		DeezerId:        deezerId,
		SpotifyId:       spotifyTrack.ID.String(),
		SpotifyTitle:    spotifyTrack.Name,
		SpotifyArtist:   spotifyTrack.Artists[0].Name,
		SpotifyDuration: spotifyTrack.TimeDuration().String(),
	}
	if useRedis {
		s.rdb.HSet(s.ctx, deezerId, "spotifyId", track.SpotifyId, "spotifyTitle", track.SpotifyTitle, "spotifyArtist", track.SpotifyArtist, "spotifyDuration", track.SpotifyDuration)
	}
	return &track, nil
}
func secondsToMinutes(inSeconds int) string {
	minutes := inSeconds / 60
	seconds := inSeconds % 60
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}

func trackConflictResolution(deezerTrack godeezer.Track, spotifyTracks []spotify.FullTrack) *spotify.FullTrack {
	if len(spotifyTracks) == 1 {
		return &spotifyTracks[0]
	}

	deezerTrackNameWithNoBrackets := strings.Trim(stripBracketsRegexp.ReplaceAllString(deezerTrack.Title, ""), " ")

	var matchingName []spotify.FullTrack
	for _, spotifyTrack := range spotifyTracks {
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

	return &spotifyTracks[0]
}

/*
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
*/
