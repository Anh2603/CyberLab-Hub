package backend

import (
	"context"
	"errors"
	"fmt"

	"github.com/Anh2603/MangaHub/internal/auth"
	"github.com/Anh2603/MangaHub/internal/store"
)

func BootstrapAdmin(ctx context.Context, accounts *store.Backend, username, password string) (bool, error) {
	if username == "" && password == "" {
		return false, nil
	}
	existing, err := accounts.FindUserByUsername(ctx, username)
	if err == nil {
		if existing.Role != "admin" {
			return false, fmt.Errorf("bootstrap username %q already belongs to a learner", username)
		}
		return false, nil
	}
	if !errors.Is(err, store.ErrUserNotFound) {
		return false, err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return false, err
	}
	if _, err := accounts.CreateUser(ctx, username, hash, "admin"); err != nil {
		return false, err
	}
	return true, nil
}
