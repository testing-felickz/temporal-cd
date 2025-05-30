package versionhistory

import (
	"go.temporal.io/api/serviceerror"
	historyspb "go.temporal.io/server/api/history/v1"
	"go.temporal.io/server/common"
)

// NewVersionHistory create a new instance of VersionHistory.
func NewVersionHistory(branchToken []byte, items []*historyspb.VersionHistoryItem) *historyspb.VersionHistory {
	return &historyspb.VersionHistory{
		BranchToken: branchToken,
		Items:       items,
	}
}

// CopyVersionHistory copies VersionHistory.
func CopyVersionHistory(v *historyspb.VersionHistory) *historyspb.VersionHistory {
	token := make([]byte, len(v.BranchToken))
	copy(token, v.BranchToken)

	items := CopyVersionHistoryItems(v.Items)

	return NewVersionHistory(token, items)
}

func CopyVersionHistoryItems(items []*historyspb.VersionHistoryItem) []*historyspb.VersionHistoryItem {
	var result []*historyspb.VersionHistoryItem
	for _, item := range items {
		result = append(result, CopyVersionHistoryItem(item))
	}
	return result
}

// CopyVersionHistoryUntilLCAVersionHistoryItem returns copy of VersionHistory up until LCA item.
func CopyVersionHistoryUntilLCAVersionHistoryItem(v *historyspb.VersionHistory, lcaItem *historyspb.VersionHistoryItem) (*historyspb.VersionHistory, error) {
	versionHistory := &historyspb.VersionHistory{}
	notFoundErr := serviceerror.NewInternal("version history does not contains the LCA item.")
	for _, item := range v.Items {
		if item.Version < lcaItem.Version {
			if err := AddOrUpdateVersionHistoryItem(versionHistory, item); err != nil {
				return nil, err
			}
		} else if item.Version == lcaItem.Version {
			if lcaItem.GetEventId() > item.GetEventId() {
				return nil, notFoundErr
			}
			if err := AddOrUpdateVersionHistoryItem(versionHistory, lcaItem); err != nil {
				return nil, err
			}
			return versionHistory, nil
		} else {
			return nil, notFoundErr
		}
	}
	return nil, notFoundErr
}

// SetVersionHistoryBranchToken sets the branch token.
func SetVersionHistoryBranchToken(v *historyspb.VersionHistory, branchToken []byte) {
	v.BranchToken = make([]byte, len(branchToken))
	copy(v.BranchToken, branchToken)
}

// AddOrUpdateVersionHistoryItem updates the VersionHistory with new VersionHistoryItem.
func AddOrUpdateVersionHistoryItem(v *historyspb.VersionHistory, item *historyspb.VersionHistoryItem) error {
	if len(v.Items) == 0 {
		v.Items = []*historyspb.VersionHistoryItem{CopyVersionHistoryItem(item)}
		return nil
	}

	lastItem := v.Items[len(v.Items)-1]
	if item.Version < lastItem.Version {
		return serviceerror.NewInternalf("cannot update version history with a lower version %v. Last version: %v", item.Version, lastItem.Version)
	}

	if item.GetEventId() <= lastItem.GetEventId() {
		return serviceerror.NewInternalf("cannot add version history with a lower event id %v. Last event id: %v", item.GetEventId(), lastItem.GetEventId())
	}

	if item.Version > lastItem.Version {
		// Add a new history
		v.Items = append(v.Items, CopyVersionHistoryItem(item))
	} else {
		// item.Version == lastItem.Version && item.EventID > lastItem.EventID
		// Update event ID
		lastItem.EventId = item.GetEventId()
	}
	return nil
}

// ContainsVersionHistoryItem check whether VersionHistory has given VersionHistoryItem.
func ContainsVersionHistoryItem(v *historyspb.VersionHistory, item *historyspb.VersionHistoryItem) bool {
	prevEventID := common.FirstEventID - 1
	for _, currentItem := range v.Items {
		if item.GetVersion() == currentItem.GetVersion() {
			if prevEventID < item.GetEventId() && item.GetEventId() <= currentItem.GetEventId() {
				return true
			}
		} else if item.GetVersion() < currentItem.GetVersion() {
			return false
		}
		prevEventID = currentItem.GetEventId()
	}
	return false
}

// FindLCAVersionHistoryItem returns the lowest common ancestor VersionHistoryItem.
func FindLCAVersionHistoryItem(v *historyspb.VersionHistory, remote *historyspb.VersionHistory) (*historyspb.VersionHistoryItem, error) {
	return FindLCAVersionHistoryItemFromItemSlice(v.Items, remote.Items)
}

func FindLCAVersionHistoryItemFromItemSlice(versionHistoryItemsA []*historyspb.VersionHistoryItem, versionHistoryItemsB []*historyspb.VersionHistoryItem) (*historyspb.VersionHistoryItem, error) {
	aIndex := len(versionHistoryItemsA) - 1
	bIndex := len(versionHistoryItemsB) - 1

	for aIndex >= 0 && bIndex >= 0 {
		aVersionItem := versionHistoryItemsA[aIndex]
		bVersionItem := versionHistoryItemsB[bIndex]

		if aVersionItem.Version == bVersionItem.Version {
			if aVersionItem.GetEventId() > bVersionItem.GetEventId() {
				return CopyVersionHistoryItem(bVersionItem), nil
			}
			return aVersionItem, nil
		} else if aVersionItem.Version > bVersionItem.Version {
			aIndex--
		} else {
			// aVersionItem.Version < bVersionItem.Version
			bIndex--
		}
	}

	return nil, serviceerror.NewInternal("version history is malformed. No joint point found.")
}

func FindLCAVersionHistoryItemFromItems(versionHistoryItemsA [][]*historyspb.VersionHistoryItem, versionHistoryItemsB []*historyspb.VersionHistoryItem) (*historyspb.VersionHistoryItem, int32, error) {
	var versionHistoryIndex int32
	var versionHistoryLength int32
	var versionHistoryItem *historyspb.VersionHistoryItem

	for index, localHistory := range versionHistoryItemsA {
		item, err := FindLCAVersionHistoryItemFromItemSlice(localHistory, versionHistoryItemsB)
		if err != nil {
			return nil, 0, err
		}

		// if not set
		if versionHistoryItem == nil ||
			// if seeing LCA item with higher event ID
			item.GetEventId() > versionHistoryItem.GetEventId() ||
			// if seeing LCA item with equal event ID but shorter history
			(item.GetEventId() == versionHistoryItem.GetEventId() && int32(len(localHistory)) < versionHistoryLength) {

			versionHistoryIndex = int32(index)
			versionHistoryLength = int32(len(localHistory))
			versionHistoryItem = item
		}
	}
	return CopyVersionHistoryItem(versionHistoryItem), versionHistoryIndex, nil
}

// IsVersionHistoryItemsInSameBranch checks if two version history items are in the same branch
func IsVersionHistoryItemsInSameBranch(versionHistoryItemsA []*historyspb.VersionHistoryItem, versionHistoryItemsB []*historyspb.VersionHistoryItem) bool {
	lowestCommonAncestor, err := FindLCAVersionHistoryItemFromItemSlice(versionHistoryItemsA, versionHistoryItemsB)
	if err != nil {
		return false
	}

	aLastItem, err := getLastVersionHistoryItem(versionHistoryItemsA)
	if err != nil {
		return false
	}

	bLastItem, err := getLastVersionHistoryItem(versionHistoryItemsB)
	if err != nil {
		return false
	}

	return lowestCommonAncestor.Equal(aLastItem) || lowestCommonAncestor.Equal(bLastItem)
}

func SplitVersionHistoryByLastLocalGeneratedItem(
	versionHistoryItems []*historyspb.VersionHistoryItem,
	initialFailoverVersion int64,
	failoverVersionIncrement int64,
) (localItems []*historyspb.VersionHistoryItem, remoteItems []*historyspb.VersionHistoryItem) {
	for i := len(versionHistoryItems) - 1; i >= 0; i-- {
		if versionHistoryItems[i].Version%failoverVersionIncrement == initialFailoverVersion {
			return versionHistoryItems[:i+1], versionHistoryItems[i+1:]
		}
	}
	return nil, versionHistoryItems
}

// IsLCAVersionHistoryItemAppendable checks if a LCA VersionHistoryItem is appendable.
func IsLCAVersionHistoryItemAppendable(v *historyspb.VersionHistory, lcaItem *historyspb.VersionHistoryItem) bool {
	if len(v.Items) == 0 {
		panic("version history not initialized")
	}
	if lcaItem == nil {
		panic("lcaItem is nil")
	}

	return IsEqualVersionHistoryItem(v.Items[len(v.Items)-1], lcaItem)
}

// GetFirstVersionHistoryItem return the first VersionHistoryItem.
func GetFirstVersionHistoryItem(v *historyspb.VersionHistory) (*historyspb.VersionHistoryItem, error) {
	if len(v.Items) == 0 {
		return nil, serviceerror.NewInternal("version history is empty.")
	}
	return CopyVersionHistoryItem(v.Items[0]), nil
}

// GetLastVersionHistoryItem return the last VersionHistoryItem.
func GetLastVersionHistoryItem(v *historyspb.VersionHistory) (*historyspb.VersionHistoryItem, error) {
	return getLastVersionHistoryItem(v.Items)
}

func getLastVersionHistoryItem(v []*historyspb.VersionHistoryItem) (*historyspb.VersionHistoryItem, error) {
	if len(v) == 0 {
		return nil, serviceerror.NewInternal("version history is empty.")
	}
	return CopyVersionHistoryItem(v[len(v)-1]), nil
}

// GetVersionHistoryEventVersion return the corresponding event version of an event ID.
func GetVersionHistoryEventVersion(v *historyspb.VersionHistory, eventID int64) (int64, error) {
	lastItem, err := GetLastVersionHistoryItem(v)
	if err != nil {
		return 0, err
	}
	if eventID < common.FirstEventID || eventID > lastItem.GetEventId() {
		return 0, serviceerror.NewInternalf("input event ID is not in range, eventID: %v", eventID)
	}

	// items are sorted by eventID & version
	// so the fist item with item event ID >= input event ID
	// the item version is the result
	for _, currentItem := range v.Items {
		if eventID <= currentItem.GetEventId() {
			return currentItem.GetVersion(), nil
		}
	}
	return 0, serviceerror.NewInternalf("input event ID is not in range, eventID: %v", eventID)
}

// IsEmptyVersionHistory indicate whether version history is empty
func IsEmptyVersionHistory(v *historyspb.VersionHistory) bool {
	return len(v.Items) == 0
}

// CompareVersionHistory compares 2 version history items
func CompareVersionHistory(v1 *historyspb.VersionHistory, v2 *historyspb.VersionHistory) (int, error) {
	lastItem1, err := GetLastVersionHistoryItem(v1)
	if err != nil {
		return 0, err
	}
	lastItem2, err := GetLastVersionHistoryItem(v2)
	if err != nil {
		return 0, err
	}
	return CompareVersionHistoryItem(lastItem1, lastItem2), nil
}
