# MY WEBSITE!
inside the deployment dir there is a docker-compose which brings up the whole site it currently includes:
- Cloudflared tunnel
- Nginx Reverse proxy to traffic to the appropriate webserver
- various go webservers


## Building the site
1. Create .env and deploy/.env
2.  build primary dockerfile `docker build -f deploy/site.dockerfile -t site:v1 .`
3. start the compose in deploy/

### Environment files
Two `.env` files, neither committed (see `.gitignore`), need to exist before the site runs:

```
# .env (repo root) — the app's own config
POSTGRES_USER=site
POSTGRES_PASSWORD=
POSTGRES_HOST=localhost        # compose overrides this in deployment to point to contianer
POSTGRES_PORT=5432

TurnstileSiteKey=
TurnstileSecret=

LinkCookieSecret=
resendEmailKey=
```
```
# deploy/.env — for primary docker compose 
TUNNEL_TOKEN=
```
```
# deploy/sync/.env - soon to be unused in favor of git.
REMOTE_USER=""
REMOTE_HOST=""
LOCAL_PATH=""
REMOTE_PATH=""
BU_PATH=""
```

nginx resolves every backend name at request time, so a backend that is down (or
the whole `izzy-game` stack being absent) just makes that one route return 502 —
nginx still starts and the rest of the site stays up.


# The transfer script (phasing out)
I use `deploy/sync/Synchronize.sh` to allow me to do all the development on my local machine then the script pushes the changes 
to the server and restarts the containers. I've also added an option to sync to my iCloud Drive, not a big deal to me 
since the project is only a few gigs now. I may only sync the posts dir in the future since the rest of the site is on
GitHub.

## Moving forward
The main advantage of the Synchronize script was it allowed me to quickly make edits to posts and html while I was getting
the site up and running. Now that is fairly developed I plan on using git to sync files, while this may seem like the 
obvious choice i had been using rsync to move the gitignored files like the many images and post .md files. 

# Other
a site called [Image to STL]("https://imagetostl.com/convert/file/step/to/gltf#convert") is used to create the .gltf file used in the 3d preview.
I also sometimes use [convert 3D](https://convert3d.org/app/compress).

# compressor.go
This is Claude's baby that is essentially a ffmpeg wrapper. syntax as follows, and the later loop and command will 
fix reoriented videos. Pass a `-f` flag to force overwriting of existing files.
```aiignore
go run compress.go ../../blog/posts/potato-gun/images;
```