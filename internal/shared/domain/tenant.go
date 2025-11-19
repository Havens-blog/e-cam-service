package domain

import (
	"fmt"
	"time"
)

// TenantStatus tenant status
type TenantStatus string

const (
	TenantStatusActive    TenantStatus = "active"
	TenantStatusInactive  TenantStatus = "inactive"
	TenantStatusSuspended TenantStatus = "suspended"
	TenantStatusDeleted   TenantStatus = "deleted"
)

// TenantSettings tenant settings
type TenantSettings struct {
	MaxCloudAccounts int               `json:"max_cloud_accounts" bson:"max_cloud_accounts"`
	MaxUsers         int               `json:"max_users" bson:"max_users"`
	MaxUserGroups    int               `json:"max_user_groups" bson:"max_user_groups"`
	AllowedProviders []CloudProvider   `json:"allowed_providers" bson:"allowed_providers"`
	Features         map[string]bool   `json:"features" bson:"features"`
	CustomFields     map[string]string `json:"custom_fields" bson:"custom_fields"`
}

// TenantMetadata tenant metadata
type TenantMetadata struct {
	Owner             string            `json:"owner" bson:"owner"`
	ContactEmail      string            `json:"contact_email" bson:"contact_email"`
	ContactPhone      string            `json:"contact_phone" bson:"contact_phone"`
	CompanyName       string            `json:"company_name" bson:"company_name"`
	Industry          string            `json:"industry" bson:"industry"`
	Region            string            `json:"region" bson:"region"`
	Tags              map[string]string `json:"tags" bson:"tags"`
	CloudAccountCount int               `json:"cloud_account_count" bson:"-"`
	UserCount         int               `json:"user_count" bson:"-"`
	UserGroupCount    int               `json:"user_group_count" bson:"-"`
}

// Tenant tenant domain model
type Tenant struct {
	ID          string         `json:"id" bson:"_id"`
	Name        string         `json:"name" bson:"name"`
	DisplayName string         `json:"display_name" bson:"display_name"`
	Description string         `json:"description" bson:"description"`
	Status      TenantStatus   `json:"status" bson:"status"`
	Settings    TenantSettings `json:"settings" bson:"settings"`
	Metadata    TenantMetadata `json:"metadata" bson:"metadata"`
	CreateTime  time.Time      `json:"create_time" bson:"create_time"`
	UpdateTime  time.Time      `json:"update_time" bson:"update_time"`
	CTime       int64          `json:"ctime" bson:"ctime"`
	UTime       int64          `json:"utime" bson:"utime"`
}

// TenantFilter tenant query filter
type TenantFilter struct {
	Keyword  string       `json:"keyword"`
	Status   TenantStatus `json:"status"`
	Industry string       `json:"industry"`
	Region   string       `json:"region"`
	Offset   int64        `json:"offset"`
	Limit    int64        `json:"limit"`
}

// CreateTenantRequest create tenant request
type CreateTenantRequest struct {
	ID          string         `json:"id" binding:"required,min=1,max=50"`
	Name        string         `json:"name" binding:"required,min=1,max=100"`
	DisplayName string         `json:"display_name" binding:"max=200"`
	Description string         `json:"description" binding:"max=500"`
	Settings    TenantSettings `json:"settings"`
	Metadata    TenantMetadata `json:"metadata"`
}

// UpdateTenantRequest update tenant request
type UpdateTenantRequest struct {
	Name        *string         `json:"name,omitempty"`
	DisplayName *string         `json:"display_name,omitempty"`
	Description *string         `json:"description,omitempty"`
	Status      *TenantStatus   `json:"status,omitempty"`
	Settings    *TenantSettings `json:"settings,omitempty"`
	Metadata    *TenantMetadata `json:"metadata,omitempty"`
}

// Validate validates tenant data
func (t *Tenant) Validate() error {
	if t.ID == "" {
		return fmt.Errorf("tenant id cannot be empty")
	}
	if len(t.ID) > 50 {
		return fmt.Errorf("tenant id length cannot exceed 50 characters")
	}
	if t.Name == "" {
		return fmt.Errorf("tenant name cannot be empty")
	}
	if len(t.Name) > 100 {
		return fmt.Errorf("tenant name length cannot exceed 100 characters")
	}
	if len(t.DisplayName) > 200 {
		return fmt.Errorf("display name length cannot exceed 200 characters")
	}
	if len(t.Description) > 500 {
		return fmt.Errorf("description length cannot exceed 500 characters")
	}
	return nil
}

// IsActive checks if tenant is active
func (t *Tenant) IsActive() bool {
	return t.Status == TenantStatusActive
}

// CanCreateCloudAccount checks if can create cloud account
func (t *Tenant) CanCreateCloudAccount() bool {
	if t.Settings.MaxCloudAccounts <= 0 {
		return true
	}
	return t.Metadata.CloudAccountCount < t.Settings.MaxCloudAccounts
}

// CanCreateUser checks if can create user
func (t *Tenant) CanCreateUser() bool {
	if t.Settings.MaxUsers <= 0 {
		return true
	}
	return t.Metadata.UserCount < t.Settings.MaxUsers
}

// CanCreateUserGroup checks if can create user group
func (t *Tenant) CanCreateUserGroup() bool {
	if t.Settings.MaxUserGroups <= 0 {
		return true
	}
	return t.Metadata.UserGroupCount < t.Settings.MaxUserGroups
}

// IsProviderAllowed checks if cloud provider is allowed
func (t *Tenant) IsProviderAllowed(provider CloudProvider) bool {
	if len(t.Settings.AllowedProviders) == 0 {
		return true
	}
	for _, p := range t.Settings.AllowedProviders {
		if p == provider {
			return true
		}
	}
	return false
}

// IsFeatureEnabled checks if feature is enabled
func (t *Tenant) IsFeatureEnabled(feature string) bool {
	if t.Settings.Features == nil {
		return false
	}
	enabled, exists := t.Settings.Features[feature]
	return exists && enabled
}
