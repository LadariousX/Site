## MY WEBSITE!
inside the deployment dir there is a docker-compose which brings up the whole site it currently includes:
- Cloudflared tunnel
- Nginx Reverse proxy to traffic to the appropriate webserver
- various webservers

### Prereqs on the host
- `docker network create shared-net` — an external bridge shared with the
  `ICS-orientation-izzyRun` stack so nginx can reach `izzy-game`. `Synchronize.sh`
  creates it on the remote if it's missing.
- Build the images once: `go-refresh:v1` (see below) and `survey-bot:v3`
  (`docker build -t survey-bot:v3 Tools/tools/Survey-bot`).

nginx resolves every backend name at request time, so a backend that is down (or
the whole `izzy-game` stack being absent) just makes that one route return 502 —
nginx still starts and the rest of the site stays up.

# Custom Go image
`go-refresh:v1` has mounted volumes which are declared in the compose file as well as a few preinstalled modules. On 
startup, it runs `go mod tidy && go run main.go` So that I don't spend all my time rebuilding images. This is the
pipeline I've chosen for my site since I'll be updating it so much. Build image with `docker build -f deploy/blog/Dockerfile -t go-refresh:v1 .` from Site/

# The transfer script
I use a `Synchronize.sh` to allow me to do all the develpment on my local machine then the script pushis the changes 
to the server and restarts the containers. Ive also added a option to sinc to my iCloud Drive, not a big deal to me 
since the project is only a few gigs now. I may only sync the posts dir in the future since the rest of the site is on
GitHub.

# Other
a site called [Image to STL]("https://imagetostl.com/convert/file/step/to/gltf#convert") is used to create the .gltf file used in the 3d preview.

# compressor.go
This is Claude's baby that is essentially a ffmpeg wrapper. syntax as follows, and the later loop and command will fix reoriented videos.
Pass a `-f` flag to force overwriting of existing files. 

```aiignore
go run compress.go ../../Blog/posts/potato-gun/images;
```

```aiignore
for i in $(seq 20 26); do
num=$(printf "%03d" $i)
ffmpeg -i "${num}.mp4" -vf "transpose=1" temp.mp4 && mv temp.mp4 "${num}.mp4"
done;
```