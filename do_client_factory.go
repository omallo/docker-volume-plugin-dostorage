package main

import (
	"github.com/digitalocean/godo"
	"golang.org/x/oauth2"
)

func NewDoAPIClient(accessToken string) *godo.Client {
	accessTokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accessToken})

	oauthClient := oauth2.NewClient(oauth2.NoContext, accessTokenSource)
	client := godo.NewClient(oauthClient)

	return client
}
