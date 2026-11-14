package main

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
)

// errGroupNameTaken means this user already has a group with that name.
var errGroupNameTaken = errors.New("group name taken")

// idSlot is one top-level place on a person's home list: an ungrouped tracker
// or a group. Members are the visible trackers in that group, in group order.
type idSlot struct {
	GroupID   uint
	TrackerID uint
	Members   []uint
}

// homeSlot is the same list, ready to render. Group is nil for an ungrouped tracker.
type homeSlot struct {
	Group *homeGroup
	Card  trackerCard
}

// homeGroup is one section header and the cards inside it.
type homeGroup struct {
	ID         uint
	Name       string
	Collapsed  bool
	Cards      []trackerCard
	CountLabel string
	CSRFToken  string // set by render
}

func trackerCountLabel(n int) string {
	if n == 1 {
		return "1 tracker"
	}
	return strconv.Itoa(n) + " trackers"
}

func groupNameProblem(name string) string {
	switch {
	case name == "":
		return "Give the group a name."
	case utf8.RuneCountInString(name) > maxTrackerName:
		return "Keep the name to " + strconv.Itoa(maxTrackerName) + " characters."
	}
	return ""
}

func groupForUser(db *gorm.DB, userID, id uint) (TrackerGroup, error) {
	var g TrackerGroup
	if id == 0 {
		return g, gorm.ErrRecordNotFound
	}
	err := db.Where("id = ? AND user_id = ?", id, userID).Take(&g).Error
	return g, err
}

func groupNameTaken(db *gorm.DB, userID, except uint, name string) (bool, error) {
	var groups []TrackerGroup
	if err := db.Where("user_id = ?", userID).Find(&groups).Error; err != nil {
		return false, err
	}
	for _, g := range groups {
		if g.ID == except {
			continue
		}
		if sameHouseholdName(g.Name, name) {
			return true, nil
		}
	}
	return false, nil
}

// orderedTrackerIDs is the viewer's active trackers in dashboard order, before
// personal groups are applied: placed TrackerOrder rows first, then the rest in
// household order. It matches dashboard when the viewer has no groups.
func orderedTrackerIDs(db *gorm.DB, userID uint) ([]uint, error) {
	var members []HouseholdMember
	if err := db.Where("user_id = ?", userID).Order("position, household_id").Find(&members).Error; err != nil {
		return nil, err
	}
	var ids []uint
	for _, m := range members {
		var part []uint
		err := db.Model(&Tracker{}).
			Where("household_id = ? AND archived_at IS NULL", m.HouseholdID).
			Order("position, id").
			Pluck("id", &part).Error
		if err != nil {
			return nil, err
		}
		ids = append(ids, part...)
	}
	var rows []TrackerOrder
	if err := db.Where("user_id = ?", userID).Find(&rows).Error; err != nil {
		return nil, err
	}
	placed := make(map[uint]int, len(rows))
	for _, row := range rows {
		placed[row.TrackerID] = row.Position
	}
	sort.SliceStable(ids, func(i, j int) bool {
		pi, iok := placed[ids[i]]
		pj, jok := placed[ids[j]]
		if iok != jok {
			return iok
		}
		return iok && pi < pj
	})
	return ids, nil
}

// layoutBlocks is the viewer's home list. A group sits at its earliest member's
// saved place. An empty group uses its own position. Trackers and groups with
// no place come after, in the order the dashboard already used.
func layoutBlocks(db *gorm.DB, userID uint, visible []uint) ([]idSlot, error) {
	var groups []TrackerGroup
	if err := db.Where("user_id = ?", userID).Order("id").Find(&groups).Error; err != nil {
		return nil, err
	}
	var items []TrackerGroupItem
	if err := db.Where("user_id = ?", userID).Find(&items).Error; err != nil {
		return nil, err
	}
	var orders []TrackerOrder
	if err := db.Where("user_id = ?", userID).Find(&orders).Error; err != nil {
		return nil, err
	}
	placed := make(map[uint]int, len(orders))
	for _, o := range orders {
		placed[o.TrackerID] = o.Position
	}
	visibleSet := make(map[uint]bool, len(visible))
	index := make(map[uint]int, len(visible))
	for i, id := range visible {
		visibleSet[id] = true
		index[id] = i
	}
	known := make(map[uint]bool, len(groups))
	for _, g := range groups {
		known[g.ID] = true
	}
	itemPos := map[uint]int{}
	members := map[uint][]uint{}
	grouped := map[uint]bool{}
	for _, it := range items {
		if !visibleSet[it.TrackerID] || !known[it.GroupID] {
			continue
		}
		grouped[it.TrackerID] = true
		itemPos[it.TrackerID] = it.Position
		members[it.GroupID] = append(members[it.GroupID], it.TrackerID)
	}
	for gid, ids := range members {
		sort.SliceStable(ids, func(i, j int) bool {
			pi, iok := placed[ids[i]]
			pj, jok := placed[ids[j]]
			if iok != jok {
				return iok
			}
			if iok && pi != pj {
				return pi < pj
			}
			if itemPos[ids[i]] != itemPos[ids[j]] {
				return itemPos[ids[i]] < itemPos[ids[j]]
			}
			return ids[i] < ids[j]
		})
		members[gid] = ids
	}

	type block struct {
		slot   idSlot
		anchor int
		placed bool
		seq    int
	}
	var blocks []block
	for _, g := range groups {
		b := block{slot: idSlot{GroupID: g.ID, Members: append([]uint(nil), members[g.ID]...)}, seq: int(g.ID)}
		ids := members[g.ID]
		if len(ids) == 0 {
			if g.Position > 0 {
				b.anchor = g.Position
				b.placed = true
			}
		} else {
			minPos := 0
			anyPlaced := false
			for _, id := range ids {
				p, ok := placed[id]
				if !ok {
					continue
				}
				if !anyPlaced || p < minPos {
					minPos = p
					anyPlaced = true
				}
			}
			if anyPlaced {
				b.anchor = minPos
				b.placed = true
			}
		}
		blocks = append(blocks, b)
	}
	for _, id := range visible {
		if grouped[id] {
			continue
		}
		b := block{slot: idSlot{TrackerID: id}, seq: index[id] + 1_000_000}
		if p, ok := placed[id]; ok {
			b.anchor = p
			b.placed = true
		}
		blocks = append(blocks, b)
	}
	sort.SliceStable(blocks, func(i, j int) bool {
		if blocks[i].placed != blocks[j].placed {
			return blocks[i].placed
		}
		if blocks[i].placed && blocks[i].anchor != blocks[j].anchor {
			return blocks[i].anchor < blocks[j].anchor
		}
		return blocks[i].seq < blocks[j].seq
	})
	out := make([]idSlot, len(blocks))
	for i, b := range blocks {
		out[i] = b.slot
	}
	return out, nil
}

func (a *app) homeSlots(userID uint, cards []trackerCard) ([]homeSlot, error) {
	ids := make([]uint, len(cards))
	byID := make(map[uint]trackerCard, len(cards))
	for i, c := range cards {
		ids[i] = c.Tracker.ID
		byID[c.Tracker.ID] = c
	}
	blocks, err := layoutBlocks(a.db, userID, ids)
	if err != nil {
		return nil, err
	}
	var groups []TrackerGroup
	if err := a.db.Where("user_id = ?", userID).Find(&groups).Error; err != nil {
		return nil, err
	}
	byGroup := make(map[uint]TrackerGroup, len(groups))
	for _, g := range groups {
		byGroup[g.ID] = g
	}
	out := make([]homeSlot, 0, len(blocks))
	for _, b := range blocks {
		if b.TrackerID != 0 {
			c, ok := byID[b.TrackerID]
			if !ok {
				continue
			}
			out = append(out, homeSlot{Card: c})
			continue
		}
		g, ok := byGroup[b.GroupID]
		if !ok {
			continue
		}
		hg := &homeGroup{ID: g.ID, Name: g.Name, Collapsed: g.Collapsed}
		for _, id := range b.Members {
			if c, ok := byID[id]; ok {
				hg.Cards = append(hg.Cards, c)
			}
		}
		hg.CountLabel = trackerCountLabel(len(hg.Cards))
		out = append(out, homeSlot{Group: hg})
	}
	return out, nil
}

func flattenSlots(slots []homeSlot) []trackerCard {
	var cards []trackerCard
	for _, s := range slots {
		if s.Group != nil {
			cards = append(cards, s.Group.Cards...)
			continue
		}
		if s.Card.Tracker.ID != 0 {
			cards = append(cards, s.Card)
		}
	}
	return cards
}

func (a *app) loadHome(u *User) (dashboardPage, error) {
	cards, err := a.dashboard(u)
	if err != nil {
		return dashboardPage{}, err
	}
	slots, err := a.homeSlots(u.ID, cards)
	if err != nil {
		return dashboardPage{}, err
	}
	if slots == nil {
		slots = []homeSlot{}
	}
	page := dashboardPage{Cards: flattenSlots(slots), Slots: slots}
	fillMarkers(&page)
	return page, nil
}

// writeSlots stores the visual list. Visible trackers get contiguous positions.
// A tracker the user cannot see right now keeps a place after that list, so a
// later restore or rejoin appends instead of landing in the middle.
func writeSlots(tx *gorm.DB, userID uint, slots []idSlot) error {
	n := 1
	type ord struct {
		id  uint
		pos int
	}
	var rows []ord
	for _, s := range slots {
		if s.TrackerID != 0 {
			rows = append(rows, ord{s.TrackerID, n})
			n++
			continue
		}
		if err := tx.Model(&TrackerGroup{}).Where("id = ? AND user_id = ?", s.GroupID, userID).Update("position", n).Error; err != nil {
			return err
		}
		for i, id := range s.Members {
			rows = append(rows, ord{id, n})
			err := tx.Model(&TrackerGroupItem{}).
				Where("user_id = ? AND tracker_id = ?", userID, id).
				Update("position", i+1).Error
			if err != nil {
				return err
			}
			n++
		}
		if len(s.Members) == 0 {
			n++
		}
	}
	var existing []TrackerOrder
	if err := tx.Where("user_id = ?", userID).Order("position, tracker_id").Find(&existing).Error; err != nil {
		return err
	}
	in := make(map[uint]bool, len(rows))
	out := make([]TrackerOrder, 0, len(rows)+len(existing))
	for _, r := range rows {
		in[r.id] = true
		out = append(out, TrackerOrder{UserID: userID, TrackerID: r.id, Position: r.pos})
	}
	next := n
	for _, e := range existing {
		if in[e.TrackerID] {
			continue
		}
		out = append(out, TrackerOrder{UserID: userID, TrackerID: e.TrackerID, Position: next})
		next++
	}
	if err := tx.Where("user_id = ?", userID).Delete(&TrackerOrder{}).Error; err != nil {
		return err
	}
	if len(out) == 0 {
		return nil
	}
	return tx.Create(&out).Error
}

func containsID(ids []uint, id uint) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func removeID(ids []uint, id uint) []uint {
	out := make([]uint, 0, len(ids))
	for _, v := range ids {
		if v != id {
			out = append(out, v)
		}
	}
	return out
}

func groupContaining(slots []idSlot, trackerID uint) uint {
	for _, s := range slots {
		if s.GroupID != 0 && containsID(s.Members, trackerID) {
			return s.GroupID
		}
	}
	return 0
}

func detachTracker(slots []idSlot, trackerID uint) []idSlot {
	out := make([]idSlot, 0, len(slots))
	for _, s := range slots {
		if s.TrackerID == trackerID {
			continue
		}
		if s.GroupID != 0 {
			s.Members = removeID(s.Members, trackerID)
		}
		out = append(out, s)
	}
	return out
}

// takeTrackerSlot puts repl where the tracker was. A tracker that was already
// in a group is not nested: the new group is inserted just after that group.
func takeTrackerSlot(slots []idSlot, trackerID uint, repl idSlot) []idSlot {
	out := make([]idSlot, 0, len(slots)+1)
	found := false
	for _, s := range slots {
		if s.TrackerID == trackerID {
			out = append(out, repl)
			found = true
			continue
		}
		if s.GroupID != 0 && containsID(s.Members, trackerID) {
			s.Members = removeID(s.Members, trackerID)
			out = append(out, s, repl)
			found = true
			continue
		}
		out = append(out, s)
	}
	if !found {
		out = append(out, repl)
	}
	return out
}

func insertAfterGroup(slots []idSlot, groupID, trackerID uint) []idSlot {
	out := make([]idSlot, 0, len(slots)+1)
	for _, s := range slots {
		out = append(out, s)
		if s.GroupID == groupID {
			out = append(out, idSlot{TrackerID: trackerID})
		}
	}
	return out
}

func expandGroup(slots []idSlot, groupID uint) []idSlot {
	out := make([]idSlot, 0, len(slots))
	for _, s := range slots {
		if s.GroupID != groupID {
			out = append(out, s)
			continue
		}
		for _, id := range s.Members {
			out = append(out, idSlot{TrackerID: id})
		}
	}
	return out
}

func (a *app) createEmptyGroup(userID uint, name string) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		taken, err := groupNameTaken(tx, userID, 0, name)
		if err != nil {
			return err
		}
		if taken {
			return errGroupNameTaken
		}
		visible, err := orderedTrackerIDs(tx, userID)
		if err != nil {
			return err
		}
		slots, err := layoutBlocks(tx, userID, visible)
		if err != nil {
			return err
		}
		g := TrackerGroup{UserID: userID, Name: name}
		if err := tx.Create(&g).Error; err != nil {
			return err
		}
		slots = append(slots, idSlot{GroupID: g.ID})
		return writeSlots(tx, userID, slots)
	})
}

func (a *app) renameGroup(userID, groupID uint, name string) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		if _, err := groupForUser(tx, userID, groupID); err != nil {
			return err
		}
		taken, err := groupNameTaken(tx, userID, groupID, name)
		if err != nil {
			return err
		}
		if taken {
			return errGroupNameTaken
		}
		return tx.Model(&TrackerGroup{}).Where("id = ? AND user_id = ?", groupID, userID).Update("name", name).Error
	})
}

func (a *app) deleteGroup(userID, groupID uint) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		if _, err := groupForUser(tx, userID, groupID); err != nil {
			return err
		}
		visible, err := orderedTrackerIDs(tx, userID)
		if err != nil {
			return err
		}
		slots, err := layoutBlocks(tx, userID, visible)
		if err != nil {
			return err
		}
		slots = expandGroup(slots, groupID)
		if err := tx.Where("user_id = ? AND group_id = ?", userID, groupID).Delete(&TrackerGroupItem{}).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ? AND user_id = ?", groupID, userID).Delete(&TrackerGroup{}).Error; err != nil {
			return err
		}
		return writeSlots(tx, userID, slots)
	})
}

func (a *app) setGroupCollapsed(userID, groupID uint, collapsed bool) error {
	if _, err := groupForUser(a.db, userID, groupID); err != nil {
		return err
	}
	// Update with a value, not a struct, so false is written.
	return a.db.Model(&TrackerGroup{}).Where("id = ? AND user_id = ?", groupID, userID).Update("collapsed", collapsed).Error
}

func (a *app) createGroupWithTracker(userID, trackerID uint, name string) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		taken, err := groupNameTaken(tx, userID, 0, name)
		if err != nil {
			return err
		}
		if taken {
			return errGroupNameTaken
		}
		visible, err := orderedTrackerIDs(tx, userID)
		if err != nil {
			return err
		}
		if !containsID(visible, trackerID) {
			return gorm.ErrRecordNotFound
		}
		slots, err := layoutBlocks(tx, userID, visible)
		if err != nil {
			return err
		}
		g := TrackerGroup{UserID: userID, Name: name}
		if err := tx.Create(&g).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND tracker_id = ?", userID, trackerID).Delete(&TrackerGroupItem{}).Error; err != nil {
			return err
		}
		item := TrackerGroupItem{UserID: userID, TrackerID: trackerID, GroupID: g.ID, Position: 1}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		slots = takeTrackerSlot(slots, trackerID, idSlot{GroupID: g.ID, Members: []uint{trackerID}})
		return writeSlots(tx, userID, slots)
	})
}

func (a *app) moveTrackerToGroup(userID, trackerID, groupID uint) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		if _, err := groupForUser(tx, userID, groupID); err != nil {
			return err
		}
		visible, err := orderedTrackerIDs(tx, userID)
		if err != nil {
			return err
		}
		if !containsID(visible, trackerID) {
			return gorm.ErrRecordNotFound
		}
		slots, err := layoutBlocks(tx, userID, visible)
		if err != nil {
			return err
		}
		if groupContaining(slots, trackerID) == groupID {
			return nil
		}
		slots = detachTracker(slots, trackerID)
		placed := false
		for i := range slots {
			if slots[i].GroupID != groupID {
				continue
			}
			slots[i].Members = append(slots[i].Members, trackerID)
			placed = true
			break
		}
		if !placed {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Where("user_id = ? AND tracker_id = ?", userID, trackerID).Delete(&TrackerGroupItem{}).Error; err != nil {
			return err
		}
		item := TrackerGroupItem{UserID: userID, TrackerID: trackerID, GroupID: groupID, Position: 1}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		return writeSlots(tx, userID, slots)
	})
}

func (a *app) clearTrackerGroup(userID, trackerID uint) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		visible, err := orderedTrackerIDs(tx, userID)
		if err != nil {
			return err
		}
		if !containsID(visible, trackerID) {
			return gorm.ErrRecordNotFound
		}
		slots, err := layoutBlocks(tx, userID, visible)
		if err != nil {
			return err
		}
		gid := groupContaining(slots, trackerID)
		if gid == 0 {
			return nil
		}
		slots = detachTracker(slots, trackerID)
		slots = insertAfterGroup(slots, gid, trackerID)
		if err := tx.Where("user_id = ? AND tracker_id = ?", userID, trackerID).Delete(&TrackerGroupItem{}).Error; err != nil {
			return err
		}
		return writeSlots(tx, userID, slots)
	})
}

type slotRef struct {
	GroupID   uint
	TrackerID uint
}

func parseSlotRefs(raw []string) ([]slotRef, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	out := make([]slotRef, 0, len(raw))
	seenT := map[uint]bool{}
	seenG := map[uint]bool{}
	for _, s := range raw {
		kind, idStr, ok := strings.Cut(s, ":")
		if !ok {
			return nil, false
		}
		n, err := strconv.ParseUint(idStr, 10, 32)
		if err != nil || n == 0 {
			return nil, false
		}
		id := uint(n)
		switch kind {
		case "t":
			if seenT[id] {
				return nil, false
			}
			seenT[id] = true
			out = append(out, slotRef{TrackerID: id})
		case "g":
			if seenG[id] {
				return nil, false
			}
			seenG[id] = true
			out = append(out, slotRef{GroupID: id})
		default:
			return nil, false
		}
	}
	return out, true
}

func applySlotRefs(slots []idSlot, groups []TrackerGroup, visible []uint, refs []slotRef) ([]idSlot, error) {
	members := map[uint][]uint{}
	grouped := map[uint]bool{}
	for _, s := range slots {
		if s.GroupID == 0 {
			continue
		}
		members[s.GroupID] = append([]uint(nil), s.Members...)
		for _, id := range s.Members {
			grouped[id] = true
		}
	}
	wantGroups := map[uint]bool{}
	for _, g := range groups {
		wantGroups[g.ID] = true
	}
	wantTrackers := map[uint]bool{}
	for _, id := range visible {
		if !grouped[id] {
			wantTrackers[id] = true
		}
	}
	gotG := map[uint]bool{}
	gotT := map[uint]bool{}
	next := make([]idSlot, 0, len(refs))
	for _, ref := range refs {
		if ref.GroupID != 0 {
			if !wantGroups[ref.GroupID] || gotG[ref.GroupID] {
				return nil, errOrderConflict
			}
			gotG[ref.GroupID] = true
			next = append(next, idSlot{GroupID: ref.GroupID, Members: members[ref.GroupID]})
			continue
		}
		if !wantTrackers[ref.TrackerID] || gotT[ref.TrackerID] {
			return nil, errOrderConflict
		}
		gotT[ref.TrackerID] = true
		next = append(next, idSlot{TrackerID: ref.TrackerID})
	}
	if len(gotG) != len(wantGroups) || len(gotT) != len(wantTrackers) {
		return nil, errOrderConflict
	}
	return next, nil
}

func (a *app) saveHomeSlots(userID uint, refs []slotRef) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		visible, err := orderedTrackerIDs(tx, userID)
		if err != nil {
			return err
		}
		slots, err := layoutBlocks(tx, userID, visible)
		if err != nil {
			return err
		}
		var groups []TrackerGroup
		if err := tx.Where("user_id = ?", userID).Find(&groups).Error; err != nil {
			return err
		}
		next, err := applySlotRefs(slots, groups, visible, refs)
		if err != nil {
			return err
		}
		return writeSlots(tx, userID, next)
	})
}

func (a *app) reorderGroupTrackers(userID, groupID uint, ids []uint) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		if _, err := groupForUser(tx, userID, groupID); err != nil {
			return err
		}
		visible, err := orderedTrackerIDs(tx, userID)
		if err != nil {
			return err
		}
		slots, err := layoutBlocks(tx, userID, visible)
		if err != nil {
			return err
		}
		found := false
		var members []uint
		for _, s := range slots {
			if s.GroupID != groupID {
				continue
			}
			members = s.Members
			found = true
		}
		if !found {
			return gorm.ErrRecordNotFound
		}
		if !sameIDSet(members, ids) {
			return errOrderConflict
		}
		for i := range slots {
			if slots[i].GroupID == groupID {
				slots[i].Members = append([]uint(nil), ids...)
			}
		}
		return writeSlots(tx, userID, slots)
	})
}

func (a *app) trackerGroupState(userID, trackerID uint) ([]TrackerGroup, uint, error) {
	var groups []TrackerGroup
	if err := a.db.Where("user_id = ?", userID).Order("position, id").Find(&groups).Error; err != nil {
		return nil, 0, err
	}
	var items []TrackerGroupItem
	if err := a.db.Where("user_id = ? AND tracker_id = ?", userID, trackerID).Limit(1).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	if len(items) == 0 {
		return groups, 0, nil
	}
	for _, g := range groups {
		if g.ID == items[0].GroupID {
			return groups, g.ID, nil
		}
	}
	return groups, 0, nil
}

func (a *app) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	name := strings.TrimSpace(r.PostFormValue("name"))
	if msg := groupNameProblem(name); msg != "" {
		a.renderGroupError(w, r, u, name, msg)
		return
	}
	err := a.createEmptyGroup(u.ID, name)
	if errors.Is(err, errGroupNameTaken) {
		a.renderGroupError(w, r, u, name, "You already have a group with that name.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	redirectTo(w, r, "/")
}

func (a *app) renderGroupError(w http.ResponseWriter, r *http.Request, u *User, name, msg string) {
	page, err := a.loadHome(u)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	page.GroupName = name
	page.GroupError = msg
	a.render(w, r, http.StatusUnprocessableEntity, "dashboard.html", page)
}

func (a *app) handleRenameGroup(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	name := strings.TrimSpace(r.PostFormValue("name"))
	if msg := groupNameProblem(name); msg != "" {
		a.message(w, r, http.StatusUnprocessableEntity, "Couldn't rename", msg)
		return
	}
	err := a.renameGroup(u.ID, pathID(r, "id"), name)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		a.lookupError(w, r, err)
		return
	}
	if errors.Is(err, errGroupNameTaken) {
		a.message(w, r, http.StatusUnprocessableEntity, "Couldn't rename", "You already have a group with that name.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	redirectTo(w, r, "/")
}

func (a *app) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	err := a.deleteGroup(u.ID, pathID(r, "id"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		a.lookupError(w, r, err)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	redirectTo(w, r, "/")
}

func (a *app) handleCollapseGroup(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	collapsed := r.PostFormValue("collapsed") == "1"
	err := a.setGroupCollapsed(u.ID, pathID(r, "id"), collapsed)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		a.lookupError(w, r, err)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *app) handleReorderGroup(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	ids, ok := orderIDs(r)
	if !ok {
		a.badOrder(w, r)
		return
	}
	err := a.reorderGroupTrackers(u.ID, pathID(r, "id"), ids)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		a.lookupError(w, r, err)
		return
	}
	a.finishOrder(w, r, err)
}

func (a *app) handleSetTrackerGroup(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	t, role, err := trackerForUser(a.db, u.ID, pathID(r, "id"))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	choice := r.PostFormValue("group")
	name := strings.TrimSpace(r.PostFormValue("group_name"))
	fail := func(msg string) {
		a.renderTrackerGroupError(w, r, t, role, u, choice, name, msg)
	}
	switch choice {
	case "", "0":
		err = a.clearTrackerGroup(u.ID, t.ID)
	case "new":
		if msg := groupNameProblem(name); msg != "" {
			fail(msg)
			return
		}
		err = a.createGroupWithTracker(u.ID, t.ID, name)
	default:
		gid := parseID(choice)
		if gid == 0 {
			fail("Pick a group.")
			return
		}
		err = a.moveTrackerToGroup(u.ID, t.ID, gid)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		a.lookupError(w, r, err)
		return
	}
	if errors.Is(err, errGroupNameTaken) {
		fail("You already have a group with that name.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	redirectTo(w, r, trackerURL(t))
}

func (a *app) renderTrackerGroupError(w http.ResponseWriter, r *http.Request, t Tracker, role string, u *User, choice, name, msg string) {
	overlay, err := a.resolveOverlay(u.ID, t, requestedOverlayID(r))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	p, err := a.trackerPage(t, role, u, overlay)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p.GroupError = msg
	p.GroupChoice = choice
	p.GroupName = name
	a.render(w, r, http.StatusUnprocessableEntity, "tracker_show.html", p)
}
