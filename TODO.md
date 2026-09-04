[//]: # (TODO:)

## blog ideas
- compile Blog back end.
- make compressor auto create thumbnails for vids.
- make hidden posts with a header mentioning not accessible from the main page
- pin posts

## Judging tool

## bugs
- if files are not updating on the site, its cloudflare cache, clear in overview.
- synchronizer aborts if one file fails to sync
- verify site title, google looks bad

## deployment TODO:
- make git public?
- preload most modules in the docker image (verify the feature works, curr refresh time is 25s)
- add rsync ignore back into script
- ensure gitignore removes Blog/static/images/raw
- standardize static routes where all begin with /static/ or /common-assets/