package main

import (
	"github.com/digitalocean/godo"
	"golang.org/x/oauth2"
)

type accessTokenSource struct {
	AccessToken string
}

func (t *accessTokenSource) Token() (*oauth2.Token, error) {
	token := &oauth2.Token{
		AccessToken: t.AccessToken,
	}
	return token, nil
}

func NewDoAPIClient(accessToken string) *godo.Client {
	accessTokenSource := &accessTokenSource{
		AccessToken: accessToken,
	}

	oauthClient := oauth2.NewClient(oauth2.NoContext, accessTokenSource)
	client := godo.NewClient(oauthClient)

	return client
}
