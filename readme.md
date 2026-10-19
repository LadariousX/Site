# MY WEBSITE!
inside the deployment dir there is a docker-compose which brings up the whole site it currently includes:
- Cloudflared tunnel
- Nginx Reverse proxy to traffic to the appropriate webserver
- postgres db
- go webserver
- survey bot 


## Building the site
1. Create .env and git deploy/.env
2. build primary dockerfile `docker build -f deploy/site.dockerfile -t site:v1 .`
3. start the compose in deploy/

## Recent versions
**v1:** latest major update where a single go app hosts the blog and various tools.

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


nginx resolves every backend name at request time, so a backend that is down (or
the whole `izzy-game` stack being absent) just makes that one route return 502 —
nginx still starts and the rest of the site stays up.

# Other
a site called [Image to STL]("https://imagetostl.com/convert/file/step/to/gltf#convert") is used to create the .gltf file used in the 3d preview.
I also sometimes use [convert 3D](https://convert3d.org/app/compress).

# compressor.go
This is Claude's baby that is essentially a ffmpeg wrapper. syntax as follows, and the later loop and command will 
fix reoriented videos. Pass a `-f` flag to force overwriting of existing files.
```aiignore
go run compress.go ../../blog/posts/potato-gun/images;
```