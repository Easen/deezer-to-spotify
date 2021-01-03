# Use

Clone the repo and install deps : 
```
go mod download
```

You have to create an app on Deezer and Spotify : 
- Deezer : https://developers.deezer.com/myapps/create
- Spotify : https://developer.spotify.com/dashboard/applications

Then copy `template.env` to `.env` and replace id / keys of apps 

You can use a Redis server to cache results from spotify (if you don't want to use it disable it on `deezerToSpotify.go` file)

```
docker run -p 6379:6379 --name some-redis -d redis
```

Then you can run : 

```
go run .
```
You will have to open links to auth yourself and create necessary tokens

