module github.com/mikehelmick/go-bananas/examples/ssr-oidc

go 1.26.4

require (
	github.com/coreos/go-oidc/v3 v3.18.0
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/sessions v1.4.0
	github.com/leonelquinteros/gotext v1.7.2
	github.com/mikehelmick/go-bananas v0.0.0
	golang.org/x/oauth2 v0.36.0
)

require (
	github.com/NYTimes/gziphandler v1.1.1 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/securecookie v1.1.2 // indirect
	github.com/sethvargo/go-envconfig v1.3.0 // indirect
	github.com/stretchr/testify v1.7.0 // indirect
	github.com/unrolled/secure v1.17.0 // indirect
)

replace github.com/mikehelmick/go-bananas => ../..
