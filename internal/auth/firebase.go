package auth

import (
	"context"
	"fmt"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
)

// FirebaseAuth wraps the Firebase Admin SDK client for server-side
// user management and third-party provider linking.
type FirebaseAuth struct {
	app    *firebase.App
	client *auth.Client
}

// NewFirebaseAuth initializes the Firebase Admin SDK.
// It expects the GOOGLE_APPLICATION_CREDENTIALS environment variable to point
// to a valid service account JSON, or runs inside an environment where
// Application Default Credentials are available.
func NewFirebaseAuth(ctx context.Context) (*FirebaseAuth, error) {
	app, err := firebase.NewApp(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("firebase.NewApp: %w", err)
	}

	client, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("firebase auth client: %w", err)
	}

	return &FirebaseAuth{
		app:    app,
		client: client,
	}, nil
}

// LinkProvider attaches third-party provider credentials to an existing Firebase
// user identified by uid. If the user does not exist, an error is returned.
func (fa *FirebaseAuth) LinkProvider(ctx context.Context, uid, providerID, accessToken string) error {
	u, err := fa.client.GetUser(ctx, uid)
	if err != nil {
		return fmt.Errorf("get user %s: %w", uid, err)
	}

	// Build provider data entry for the third-party identity.
	pd := &auth.UserProvider{
		ProviderID: providerID,
		UID:        uid,
	}

	// Merge with existing providers to avoid duplicates.
	providers := make([]*auth.UserProvider, 0, len(u.ProviderData)+1)
	seen := make(map[string]bool)
	for _, existing := range u.ProviderData {
		if existing != nil && !seen[existing.ProviderID] {
			providers = append(providers, existing)
			seen[existing.ProviderID] = true
		}
	}
	if !seen[providerID] {
		providers = append(providers, pd)
		seen[providerID] = true
	}

	_, err = fa.client.UpdateUser(ctx, uid, (&auth.UserToUpdate{}).
		ProviderData(providers))
	if err != nil {
		return fmt.Errorf("update user providers: %w", err)
	}

	// accessToken is intentionally accepted but not persisted here;
	// the caller (plugin coordinator) is responsible for token storage.
	_ = accessToken
	return nil
}
