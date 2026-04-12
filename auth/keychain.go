package auth

import (
	"github.com/keybase/go-keychain"
)

// KeychainStore implements CredentialStore using macOS Keychain.
type KeychainStore struct {
	service string
}

// NewKeychainStore creates a new Keychain-based credential store.
func NewKeychainStore(service string) *KeychainStore {
	return &KeychainStore{service: service}
}

// Get retrieves a credential from the Keychain.
func (ks *KeychainStore) Get(key string) (string, error) {
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(ks.service)
	item.SetAccount(key)
	item.SetReturnData(true)

	results, err := keychain.QueryItem(item)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "", ErrNotFound
	}

	return string(results[0].Data), nil
}

// Set stores a credential in the Keychain.
func (ks *KeychainStore) Set(key, value string) error {
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(ks.service)
	item.SetAccount(key)
	item.SetData([]byte(value))
	item.SetAccessible(keychain.AccessibleAfterFirstUnlock)

	// Try to delete existing item first
	_ = keychain.DeleteItem(item)

	// Now add the new item
	return keychain.AddItem(item)
}

// Delete removes a credential from the Keychain.
func (ks *KeychainStore) Delete(key string) error {
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(ks.service)
	item.SetAccount(key)

	return keychain.DeleteItem(item)
}
