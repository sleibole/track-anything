package main

import (
	"errors"
	"net/http"

	"gorm.io/gorm"
)

const (
	guestClaimDone       = "done"
	guestClaimClosed     = "closed"
	guestClaimSuperseded = "superseded"
	guestClaimTaken      = "taken"
	guestClaimStale      = "stale"
)

func (a *app) confirmGuestEmail(tokenHash string) (string, uint, string, error) {
	var result, next string
	var userID uint
	err := a.withImmediate(func(tx *gorm.DB) error {
		now := a.now()
		res := tx.Model(&VerificationToken{}).
			Where("token_hash = ? AND used_at IS NULL AND expires_at > ?", tokenHash, now).
			Update("used_at", now)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			result = guestClaimClosed
			return nil
		}
		var vt VerificationToken
		if err := tx.Take(&vt, "token_hash = ?", tokenHash).Error; err != nil {
			return err
		}
		userID = vt.UserID
		if vt.ClaimID != nil {
			var claim GuestClaim
			if err := tx.Take(&claim, *vt.ClaimID).Error; err != nil {
				return err
			}
			next = claim.Next
			if claim.CancelledAt != nil {
				result = guestClaimSuperseded
				return nil
			}
		}
		var u User
		if err := tx.Take(&u, vt.UserID).Error; err != nil {
			return err
		}
		if !u.IsGuest() {
			if u.Email == vt.Email && u.HasVerifiedEmail() {
				result = guestClaimDone
			} else {
				result = guestClaimStale
			}
			return nil
		}
		var n int64
		if err := tx.Model(&User{}).Where("email = ? AND id <> ?", vt.Email, u.ID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			result = guestClaimTaken
			return nil
		}
		res = tx.Model(&User{}).Where("id = ? AND email = ''", u.ID).Updates(map[string]any{
			"email": vt.Email, "email_verified_at": now, "password_hash": "",
		})
		if res.Error != nil {
			if isUniqueConstraint(res.Error) {
				result = guestClaimTaken
				return nil
			}
			return res.Error
		}
		if res.RowsAffected == 0 {
			result = guestClaimStale
			return nil
		}
		if err := tx.Where("user_id = ?", u.ID).Delete(&Session{}).Error; err != nil {
			return err
		}
		if err := a.consumeStartAttempts(tx, u.ID, now); err != nil {
			return err
		}
		if vt.ClaimID != nil {
			if err := tx.Model(&GuestClaim{}).Where("id = ?", *vt.ClaimID).Update("consumed_at", now).Error; err != nil {
				return err
			}
		}
		if err := a.supersedeGuestClaims(tx, u.ID, now); err != nil {
			return err
		}
		result = guestClaimDone
		return nil
	})
	return result, userID, next, err
}

func (a *app) markDestVerified(intentID, destUserID uint) error {
	now := a.now()
	return a.withImmediate(func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE transfer_intents SET dest_verified_at = ?
			WHERE id = ? AND dest_user_id = ? AND dest_verified_at IS NULL
			AND cancelled_at IS NULL AND consumed_at IS NULL AND expires_at > ?
			AND EXISTS (SELECT 1 FROM users WHERE users.id = transfer_intents.source_user_id AND users.email = '')`,
			now, intentID, destUserID, now).Error
	})
}

type transferPage struct {
	Ready        bool
	NeedDest     bool
	NeedSource   bool
	WrongAccount bool
	Ended        bool
	DestEmail    string
}

func (a *app) handleTransferForm(w http.ResponseWriter, r *http.Request) {
	page, err := a.transferState(r)
	if errors.Is(err, errTransferDone) {
		redirectDocument(w, r, "/?moved=1")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, "account_transfer.html", page)
}

func (a *app) handleTransfer(w http.ResponseWriter, r *http.Request) {
	if r.PostFormValue("confirm") != "move" {
		page, err := a.transferState(r)
		if err != nil && !errors.Is(err, errTransferDone) {
			a.serverError(w, r, err)
			return
		}
		a.render(w, r, http.StatusUnprocessableEntity, "account_transfer.html", page)
		return
	}
	u := currentUser(r)
	raw := ""
	if c, err := r.Cookie(transferCookie); err == nil {
		raw = c.Value
	}
	err := a.withImmediate(func(tx *gorm.DB) error {
		return a.transferGuest(tx, u, raw)
	})
	if errors.Is(err, errTransferDone) {
		redirectDocument(w, r, "/?moved=1")
		return
	}
	if errors.Is(err, errTransferDeny) || errors.Is(err, errTransferBlock) {
		page, stateErr := a.transferState(r)
		if stateErr != nil && !errors.Is(stateErr, errTransferDone) {
			a.serverError(w, r, stateErr)
			return
		}
		a.render(w, r, http.StatusForbidden, "account_transfer.html", page)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if u.ID != 0 && u.IsGuest() {
		// The guest session was deleted inside the move. Sign in as the destination.
		var receipt TransferReceipt
		if c, cerr := r.Cookie(transferCookie); cerr == nil {
			_ = a.db.Take(&receipt, "source_proof_hash = ?", hashToken(c.Value)).Error
		}
		if receipt.DestUserID != 0 {
			if err := a.startSession(w, receipt.DestUserID); err != nil {
				a.serverError(w, r, err)
				return
			}
		}
	}
	a.clearFlowCookie(w, transferCookie)
	redirectDocument(w, r, "/?moved=1")
}

func (a *app) transferState(r *http.Request) (transferPage, error) {
	u := currentUser(r)
	c, err := r.Cookie(transferCookie)
	if err != nil || c.Value == "" {
		return transferPage{NeedSource: true}, nil
	}
	hash := hashToken(c.Value)
	var receipt TransferReceipt
	err = a.db.Take(&receipt, "source_proof_hash = ?", hash).Error
	if err == nil {
		if u.ID != receipt.DestUserID {
			return transferPage{WrongAccount: true}, nil
		}
		return transferPage{}, errTransferDone
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return transferPage{}, err
	}
	var intent TransferIntent
	if err := a.db.Take(&intent, "source_proof_hash = ?", hash).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return transferPage{Ended: true}, nil
		}
		return transferPage{}, err
	}
	now := a.now()
	if intent.CancelledAt != nil || intent.ConsumedAt != nil || !now.Before(intent.ExpiresAt) {
		return transferPage{Ended: true}, nil
	}
	if u.ID != intent.SourceUserID && u.ID != intent.DestUserID {
		return transferPage{WrongAccount: true}, nil
	}
	var dest User
	if err := a.db.Take(&dest, intent.DestUserID).Error; err != nil {
		return transferPage{}, err
	}
	page := transferPage{DestEmail: dest.Email}
	if intent.DestVerifiedAt == nil {
		page.NeedDest = true
		return page, nil
	}
	var n int64
	if err := a.db.Model(&Session{}).Where("id = ? AND user_id = ?", intent.SourceSessionID, intent.SourceUserID).Count(&n).Error; err != nil {
		return transferPage{}, err
	}
	if n == 0 {
		page.Ended = true
		return page, nil
	}
	var guest User
	if err := a.db.Take(&guest, intent.SourceUserID).Error; err != nil {
		return transferPage{}, err
	}
	if !guest.IsGuest() {
		page.Ended = true
		return page, nil
	}
	page.Ready = true
	return page, nil
}

func (a *app) transferGuest(tx *gorm.DB, actor *User, rawProof string) error {
	if rawProof == "" {
		return errTransferDeny
	}
	now := a.now()
	hash := hashToken(rawProof)
	var receipt TransferReceipt
	err := tx.Take(&receipt, "source_proof_hash = ?", hash).Error
	if err == nil {
		if actor.ID != receipt.DestUserID {
			return errTransferDeny
		}
		return errTransferDone
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var intent TransferIntent
	if err := tx.Take(&intent, "source_proof_hash = ?", hash).Error; err != nil {
		return errTransferDeny
	}
	if intent.CancelledAt != nil || intent.ConsumedAt != nil || !now.Before(intent.ExpiresAt) || intent.DestVerifiedAt == nil {
		return errTransferDeny
	}
	if actor.ID != intent.SourceUserID && actor.ID != intent.DestUserID {
		return errTransferDeny
	}
	var guest User
	if err := tx.Take(&guest, intent.SourceUserID).Error; err != nil {
		return err
	}
	if !guest.IsGuest() {
		return errTransferDeny
	}
	var n int64
	if err := tx.Model(&Session{}).Where("id = ? AND user_id = ?", intent.SourceSessionID, intent.SourceUserID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return errTransferDeny
	}
	if err := moveGuestRecords(tx, guest.ID, intent.DestUserID); err != nil {
		return err
	}
	if err := tx.Create(&TransferReceipt{SourceProofHash: hash, DestUserID: intent.DestUserID, CompletedAt: now}).Error; err != nil {
		return err
	}
	if err := tx.Model(&TransferIntent{}).Where("id = ? AND consumed_at IS NULL", intent.ID).Update("consumed_at", now).Error; err != nil {
		return err
	}
	if err := tx.Model(&GuestClaim{}).Where("id = ?", intent.ClaimID).Update("consumed_at", now).Error; err != nil {
		return err
	}
	if err := a.consumeStartAttempts(tx, guest.ID, now); err != nil {
		return err
	}
	if err := a.supersedeGuestClaims(tx, guest.ID, now); err != nil {
		return err
	}
	if err := tx.Where("user_id = ?", guest.ID).Delete(&Session{}).Error; err != nil {
		return err
	}
	if err := tx.Where("user_id = ?", guest.ID).Delete(&VerificationToken{}).Error; err != nil {
		return err
	}
	if err := tx.Where("user_id = ?", guest.ID).Delete(&LoginToken{}).Error; err != nil {
		return err
	}
	if err := tx.Where("user_id = ?", guest.ID).Delete(&StartAttempt{}).Error; err != nil {
		return err
	}
	return tx.Delete(&User{}, guest.ID).Error
}

func moveGuestRecords(tx *gorm.DB, guestID, destID uint) error {
	guestHouse, err := earliestOwnedHousehold(tx, guestID)
	if err != nil {
		return err
	}
	destHouse, err := earliestOwnedHousehold(tx, destID)
	if err != nil {
		return err
	}
	if err := moveTrackers(tx, guestHouse.ID, destHouse.ID); err != nil {
		return err
	}
	if err := tx.Where("household_id = ? AND user_id = ?", guestHouse.ID, guestID).Delete(&HouseholdMember{}).Error; err != nil {
		return err
	}
	if err := deleteHouseholdIfEmpty(tx, guestHouse.ID); err != nil {
		return err
	}
	owned, err := ownedHouseholds(tx, guestID)
	if err != nil {
		return err
	}
	destOwned, err := ownedHouseholds(tx, destID)
	if err != nil {
		return err
	}
	for _, h := range owned {
		if h.ID == guestHouse.ID {
			continue
		}
		var others int64
		if err := tx.Model(&HouseholdMember{}).Where("household_id = ? AND user_id <> ?", h.ID, guestID).Count(&others).Error; err != nil {
			return err
		}
		if ownedNameTaken(destOwned, 0, h.Name) {
			if others > 0 {
				return errTransferBlock
			}
			var into Household
			for _, d := range destOwned {
				if sameHouseholdName(d.Name, h.Name) {
					into = d
					break
				}
			}
			if into.ID == 0 {
				return errTransferBlock
			}
			if err := moveTrackers(tx, h.ID, into.ID); err != nil {
				return err
			}
			if err := tx.Where("household_id = ? AND user_id = ?", h.ID, guestID).Delete(&HouseholdMember{}).Error; err != nil {
				return err
			}
			if err := deleteHouseholdIfEmpty(tx, h.ID); err != nil {
				return err
			}
			continue
		}
		pos, err := nextMemberPosition(tx, destID)
		if err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE household_members SET user_id = ?, position = ? WHERE household_id = ? AND user_id = ?`,
			destID, pos, h.ID, guestID).Error; err != nil {
			return err
		}
	}
	var rest []HouseholdMember
	if err := tx.Where("user_id = ?", guestID).Find(&rest).Error; err != nil {
		return err
	}
	for _, m := range rest {
		var n int64
		if err := tx.Model(&HouseholdMember{}).Where("household_id = ? AND user_id = ?", m.HouseholdID, destID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			if err := tx.Where("household_id = ? AND user_id = ?", m.HouseholdID, guestID).Delete(&HouseholdMember{}).Error; err != nil {
				return err
			}
			continue
		}
		pos, err := nextMemberPosition(tx, destID)
		if err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE household_members SET user_id = ?, position = ? WHERE household_id = ? AND user_id = ?`,
			destID, pos, m.HouseholdID, guestID).Error; err != nil {
			return err
		}
	}
	if err := reassignOrders(tx, guestID, destID); err != nil {
		return err
	}
	if err := tx.Model(&Entry{}).Where("recorded_by_id = ?", guestID).Update("recorded_by_id", destID).Error; err != nil {
		return err
	}
	if err := tx.Model(&RecordedZero{}).Where("recorded_by_id = ?", guestID).Update("recorded_by_id", destID).Error; err != nil {
		return err
	}
	if err := tx.Model(&Redemption{}).Where("user_id = ?", guestID).Update("user_id", destID).Error; err != nil {
		return err
	}
	return tx.Model(&TrackerCreate{}).Where("user_id = ?", guestID).Update("user_id", destID).Error
}

func earliestOwnedHousehold(tx *gorm.DB, userID uint) (Household, error) {
	var h Household
	err := tx.Table("households").Select("households.*").
		Joins("JOIN household_members ON household_members.household_id = households.id").
		Where("household_members.user_id = ? AND household_members.role = ?", userID, roleOwner).
		Order("households.created_at, households.id").
		Take(&h).Error
	return h, err
}

func moveTrackers(tx *gorm.DB, from, to uint) error {
	var last int
	if err := tx.Model(&Tracker{}).Where("household_id = ?", to).Select("COALESCE(MAX(position), 0)").Scan(&last).Error; err != nil {
		return err
	}
	var trackers []Tracker
	if err := tx.Where("household_id = ?", from).Order("position, id").Find(&trackers).Error; err != nil {
		return err
	}
	for i, t := range trackers {
		if err := tx.Model(&Tracker{}).Where("id = ?", t.ID).Updates(map[string]any{
			"household_id": to, "position": last + i + 1,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func deleteHouseholdIfEmpty(tx *gorm.DB, id uint) error {
	var members, trackers int64
	if err := tx.Model(&HouseholdMember{}).Where("household_id = ?", id).Count(&members).Error; err != nil {
		return err
	}
	if err := tx.Model(&Tracker{}).Where("household_id = ?", id).Count(&trackers).Error; err != nil {
		return err
	}
	if members == 0 && trackers == 0 {
		return tx.Delete(&Household{}, id).Error
	}
	return nil
}

func reassignOrders(tx *gorm.DB, guestID, destID uint) error {
	var orders []TrackerOrder
	if err := tx.Where("user_id = ?", guestID).Find(&orders).Error; err != nil {
		return err
	}
	for _, o := range orders {
		var n int64
		if err := tx.Model(&TrackerOrder{}).Where("user_id = ? AND tracker_id = ?", destID, o.TrackerID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			if err := tx.Where("user_id = ? AND tracker_id = ?", guestID, o.TrackerID).Delete(&TrackerOrder{}).Error; err != nil {
				return err
			}
			continue
		}
		if err := tx.Exec(`UPDATE tracker_orders SET user_id = ? WHERE user_id = ? AND tracker_id = ?`, destID, guestID, o.TrackerID).Error; err != nil {
			return err
		}
	}
	return nil
}
