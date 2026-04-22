## MY WEBSITE!
inside the deployment dir there is a docker-compose which brings up the whole site it currently includes:
- Cloudflared tunnel
- Nginx Reverse proxy to traffic to the appropriate webserver
- various webservers

# Custom Go image
`go-refresh:v0` has mounted volumes which are declared in the compose file. On startup, it runs `go mod tidy && go run main.go`
So that I don't spend all my time building images. This is the pipeline I've chosen for my site since I'll be updating 
it so much

# The transfer script
I plan to create a sh script I can run that will sync the media posts folder or entire project to the server. I still 
plan on using GitHub, but the larger files need their own solution.