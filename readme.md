## MY WEBSITE!
inside the deployment dir there is a docker-compose which brings up the whole site it currently includes:
- Cloudflared tunnel
- Nginx Reverse proxy to traffic to the appropriate webserver
- various webservers

# Custom Go image
`go-refresh:v1` has mounted volumes which are declared in the compose file as well as a few preinstalled modules. On 
startup, it runs `go mod tidy && go run main.go` So that I don't spend all my time rebuilding images. This is the
pipeline I've chosen for my site since I'll be updating it so much. Build image with `docker build -f deploy/blog/Dockerfile -t go-refresh:v1 .` from Site/

# The transfer script
I use a `Synchronize.sh` to allow me to do all the develpment on my local machine then the script pushis the changes 
to the server and restarts the containers. Ive also added a option to sinc to my iCloud Drive, not a big deal to me 
since the project is only a few gigs now. I may only sync the posts dir in the future since the rest of the site is on
GitHub.
