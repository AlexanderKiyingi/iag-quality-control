package main

import (
	"context"
	"log"
	"time"

	platformserviceauth "github.com/alvor-technologies/iag-platform-go/serviceauth"

	"iag-quality-control/backend/internal/config"
	"iag-quality-control/backend/internal/models"
)

func registerPermissionsLoop(ctx context.Context, cfg config.Config) {
	if cfg.ServiceClientSecret == "" {
		return
	}
	saClient := platformserviceauth.NewClient(platformserviceauth.Options{
		TokenURL:     cfg.AuthTokenURL,
		ClientID:     cfg.ServiceClientID,
		ClientSecret: cfg.ServiceClientSecret,
		Audience:     "iag.authentication",
	})
	descriptors := models.PermissionDescriptors()
	perms := make([]platformserviceauth.Permission, 0, len(descriptors))
	for _, d := range descriptors {
		perms = append(perms, platformserviceauth.Permission{Name: d.Name, Description: d.Description})
	}
	backoff := time.Second
	for {
		regCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := platformserviceauth.RegisterPermissions(regCtx, saClient, cfg.JWTIssuer, "quality-control", perms)
		cancel()
		if err == nil {
			log.Printf("quality-control: registered %d permissions", len(perms))
			return
		}
		log.Printf("quality-control: permissions register failed: %v (retry in %s)", err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 5*time.Minute {
			backoff *= 2
		}
	}
}
