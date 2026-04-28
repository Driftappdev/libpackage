module github.com/platformcore/libpackage/security

go 1.25.1

require (
	github.com/golang-jwt/jwt/v5 v5.3.1
	golang.org/x/crypto v0.50.0
	golang.org/x/oauth2 v0.35.0
)

replace github.com/platformcore/libpackage/plugins => ../plugins

