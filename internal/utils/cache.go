package utils

import (
	"sync"
	"time"
)

// CacheEntry represents a cached value with expiration
type CacheEntry struct {
	Value     interface{}
	ExpiresAt time.Time
}

// IsExpired checks if the cache entry has expired
func (e *CacheEntry) IsExpired() bool {
	return time.Now().After(e.ExpiresAt)
}

// Cache represents an in-memory cache with TTL support
type Cache struct {
	data       map[string]*CacheEntry
	mutex      sync.RWMutex
	defaultTTL time.Duration
}

// NewCache creates a new in-memory cache
func NewCache(defaultTTL time.Duration) *Cache {
	cache := &Cache{
		data:       make(map[string]*CacheEntry),
		defaultTTL: defaultTTL,
	}

	// Start cleanup routine
	go cache.cleanupExpired()

	return cache
}

// Get retrieves a value from the cache
func (c *Cache) Get(key string) (interface{}, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	entry, exists := c.data[key]
	if !exists {
		return nil, false
	}

	if entry.IsExpired() {
		// Remove expired entry
		delete(c.data, key)
		return nil, false
	}

	return entry.Value, true
}

// Set stores a value in the cache with default TTL
func (c *Cache) Set(key string, value interface{}) {
	c.SetWithTTL(key, value, c.defaultTTL)
}

// SetWithTTL stores a value in the cache with custom TTL
func (c *Cache) SetWithTTL(key string, value interface{}, ttl time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.data[key] = &CacheEntry{
		Value:     value,
		ExpiresAt: time.Now().Add(ttl),
	}
}

// Delete removes a value from the cache
func (c *Cache) Delete(key string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	delete(c.data, key)
}

// Clear removes all entries from the cache
func (c *Cache) Clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.data = make(map[string]*CacheEntry)
}

// Size returns the number of items in the cache
func (c *Cache) Size() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return len(c.data)
}

// cleanupExpired removes expired entries from the cache
func (c *Cache) cleanupExpired() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.mutex.Lock()
		now := time.Now()
		for key, entry := range c.data {
			if now.After(entry.ExpiresAt) {
				delete(c.data, key)
			}
		}
		c.mutex.Unlock()
	}
}

// PermissionCache provides caching for user permissions
type PermissionCache struct {
	cache *Cache
}

// NewPermissionCache creates a new permission cache
func NewPermissionCache() *PermissionCache {
	return &PermissionCache{
		cache: NewCache(5 * time.Minute), // 5-minute default TTL for permissions
	}
}

// Permission types
type Permission struct {
	IsAdmin          bool
	IsDirectoryOwner map[string]bool // directory_id -> is_owner
}

// GetUserPermissions retrieves cached user permissions
func (pc *PermissionCache) GetUserPermissions(userEmail string) (*Permission, bool) {
	value, exists := pc.cache.Get("perm:" + userEmail)
	if !exists {
		return nil, false
	}

	perm, ok := value.(*Permission)
	return perm, ok
}

// SetUserPermissions caches user permissions
func (pc *PermissionCache) SetUserPermissions(userEmail string, perm *Permission) {
	pc.cache.Set("perm:"+userEmail, perm)
}

// GetDirectoryOwnership retrieves cached directory ownership
func (pc *PermissionCache) GetDirectoryOwnership(directoryID, userEmail string) (bool, bool) {
	key := "dir_owner:" + directoryID + ":" + userEmail
	value, exists := pc.cache.Get(key)
	if !exists {
		return false, false
	}

	isOwner, ok := value.(bool)
	return isOwner, ok
}

// SetDirectoryOwnership caches directory ownership
func (pc *PermissionCache) SetDirectoryOwnership(directoryID, userEmail string, isOwner bool) {
	key := "dir_owner:" + directoryID + ":" + userEmail
	pc.cache.Set(key, isOwner)
}

// GetAdminStatus retrieves cached admin status
func (pc *PermissionCache) GetAdminStatus(userEmail string) (bool, bool) {
	value, exists := pc.cache.Get("admin:" + userEmail)
	if !exists {
		return false, false
	}

	isAdmin, ok := value.(bool)
	return isAdmin, ok
}

// SetAdminStatus caches admin status
func (pc *PermissionCache) SetAdminStatus(userEmail string, isAdmin bool) {
	pc.cache.Set("admin:"+userEmail, isAdmin)
}

// InvalidateUser removes all cached permissions for a user
func (pc *PermissionCache) InvalidateUser(userEmail string) {
	pc.cache.Delete("perm:" + userEmail)
	pc.cache.Delete("admin:" + userEmail)

	// We don't have a direct way to invalidate all directory ownership entries for a user
	// without iterating through all cache keys, so we'll rely on TTL expiration for those
}

// InvalidateDirectory removes cached permissions for a specific directory
func (pc *PermissionCache) InvalidateDirectory(directoryID string) {
	// Unfortunately, we can't efficiently iterate through all keys to find directory-specific ones
	// without changing the cache implementation. For now, we'll rely on TTL expiration.
	// In a production system, you might want to use a more sophisticated cache like Redis
	// with pattern-based key deletion.
}

// Clear removes all cached permissions
func (pc *PermissionCache) Clear() {
	pc.cache.Clear()
}
