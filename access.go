package main

import (
	"gorm.io/gorm"
)

// Every tracker lookup goes through one of these, so access rules live in one place.
// A tracker someone can't see is gorm.ErrRecordNotFound, which handlers turn into a 404.

// trackerForUser returns an active tracker and the user's role in its household.
func trackerForUser(db *gorm.DB, userID, trackerID uint) (Tracker, string, error) {
	var row struct {
		Tracker
		Role string
	}
	err := db.Table("trackers").
		Select("trackers.*, household_members.role").
		Joins("JOIN household_members ON household_members.household_id = trackers.household_id").
		Where("trackers.id = ? AND household_members.user_id = ? AND trackers.archived_at IS NULL", trackerID, userID).
		Take(&row).Error
	return row.Tracker, row.Role, err
}

// trackerForShareToken returns the tracker behind a share link, if the link is still on.
func trackerForShareToken(db *gorm.DB, token string) (Tracker, error) {
	var t Tracker
	if token == "" {
		return t, gorm.ErrRecordNotFound
	}
	err := db.Take(&t, "share_token = ? AND archived_at IS NULL", token).Error
	return t, err
}

// archivedTrackerForOwner returns an archived tracker only if the user owns its household.
// Only restore uses it; every other route sees archived trackers as missing.
func archivedTrackerForOwner(db *gorm.DB, userID, trackerID uint) (Tracker, error) {
	var t Tracker
	err := db.Table("trackers").
		Select("trackers.*").
		Joins("JOIN household_members ON household_members.household_id = trackers.household_id").
		Where("trackers.id = ? AND household_members.user_id = ? AND household_members.role = ? AND trackers.archived_at IS NOT NULL",
			trackerID, userID, roleOwner).
		Take(&t).Error
	return t, err
}

// ownedHouseholds returns the households the user owns, oldest membership first.
func ownedHouseholds(db *gorm.DB, userID uint) ([]Household, error) {
	var hs []Household
	err := db.Table("households").
		Select("households.*").
		Joins("JOIN household_members ON household_members.household_id = households.id").
		Where("household_members.user_id = ? AND household_members.role = ?", userID, roleOwner).
		Order("household_members.created_at, households.id").
		Find(&hs).Error
	return hs, err
}

// visibleCountTrackers returns active count trackers the user can see, except skipID.
// Archived trackers and other kinds are left out. Order is name, then id.
func visibleCountTrackers(db *gorm.DB, userID, skipID uint) ([]Tracker, error) {
	var trackers []Tracker
	err := db.Table("trackers").
		Select("trackers.*").
		Joins("JOIN household_members ON household_members.household_id = trackers.household_id").
		Where("household_members.user_id = ? AND trackers.archived_at IS NULL AND trackers.kind = ? AND trackers.id <> ?",
			userID, "count", skipID).
		Order("trackers.name, trackers.id").
		Find(&trackers).Error
	return trackers, err
}

// householdForUser returns a household and the user's role in it.
func householdForUser(db *gorm.DB, userID, householdID uint) (Household, string, error) {
	var h Household
	var m HouseholdMember
	if err := db.Take(&m, "household_id = ? AND user_id = ?", householdID, userID).Error; err != nil {
		return h, "", err
	}
	err := db.Take(&h, householdID).Error
	return h, m.Role, err
}

// householdCreator is the household's first owner. Owners can't be removed, so it is
// the person who created it. Share pages use this person's zone for "today".
func householdCreator(db *gorm.DB, householdID uint) (User, error) {
	var u User
	err := db.Table("users").
		Select("users.*").
		Joins("JOIN household_members ON household_members.user_id = users.id").
		Where("household_members.household_id = ? AND household_members.role = ?", householdID, roleOwner).
		Order("household_members.created_at, users.id").
		Take(&u).Error
	return u, err
}
