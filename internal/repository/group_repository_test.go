package repository

import (
	"fmt"
	"go-chat-room/internal/model"
	"go-chat-room/pkg/config"
	"go-chat-room/pkg/db"
	"go-chat-room/pkg/logger"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// --- Test Setup ---

// setupTestGroups initializes DB and returns repositories needed for group tests.
func setupTestGroups(t *testing.T) (*GroupRepository, *GroupMemberRepository, *UserRepository) {
	// Initialize Config, Logger, DB
	// Assuming serial execution or thread-safe initialization elsewhere if needed
	if err := config.InitTest(); err != nil {
		t.Fatalf("Failed to initialize config: %v", err)
	}
	if logger.L == nil {
		err := logger.InitLogger(config.GlobalConfig.Log.Level, config.GlobalConfig.Log.ProductionMode)
		if err != nil {
			t.Logf("Logger init failed (using default): %v", err)
		}
	}
	err := db.InitDB()
	require.NoError(t, err, "Failed to connect to test database")

	// Cleanup functions
	t.Cleanup(func() { cleanupGroupMemberTable(t) })
	t.Cleanup(func() { cleanupGroupTable(t) })
	t.Cleanup(func() { cleanupUserTable(t) }) // Also cleanup users

	// Create repositories
	groupRepo := NewGroupRepository()
	groupMemberRepo := NewGroupMemberRepository()
	userRepo := NewUserRepository()

	return groupRepo, groupMemberRepo, userRepo
}

// Helper to cleanup groups table
func cleanupGroupTable(t *testing.T) {
	if err := db.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.Group{}).Error; err != nil {
		t.Logf("Warning: Failed to cleanup groups table: %v", err)
	} else {
		t.Log("Successfully cleaned up groups table.")
	}
}

// Helper to cleanup group_members table
func cleanupGroupMemberTable(t *testing.T) {
	if err := db.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.GroupMember{}).Error; err != nil {
		t.Logf("Warning: Failed to cleanup group_members table: %v", err)
	} else {
		t.Log("Successfully cleaned up group_members table.")
	}
}

// Helper to create test users (copied from user_repository_test.go or similar)
func createTestUserForGroup(t *testing.T, userRepo *UserRepository, username string) *model.User {
	user := &model.User{
		Username: username,
		Password: "testpassword",
		Email:    fmt.Sprintf("%s@example.com", username),
		Avatar:   "default.png",
	}
	err := userRepo.Create(user)
	require.NoError(t, err, "Failed to create test user %s", username)
	require.True(t, user.ID > 0)
	t.Logf("Created test user '%s' with ID: %d", username, user.ID)
	return user
}

// --- Tests ---

func TestGroupRepository_Create(t *testing.T) {
	groupRepo, groupMemberRepo, userRepo := setupTestGroups(t)
	owner := createTestUserForGroup(t, userRepo, "groupOwner1")

	group := &model.Group{
		Name:    "Test Group Alpha",
		OwnerID: owner.ID,
	}

	err := groupRepo.Create(group)
	require.NoError(t, err)
	assert.True(t, group.ID > 0, "Group ID should be set after creation")

	// Verify group exists in DB
	foundGroup, err := groupRepo.FindByID(group.ID)
	require.NoError(t, err)
	require.NotNil(t, foundGroup)
	assert.Equal(t, group.Name, foundGroup.Name)
	assert.Equal(t, owner.ID, foundGroup.OwnerID)

	// Verify owner is automatically added as a member with 'owner' role
	ownerMember, err := groupMemberRepo.FindMember(group.ID, owner.ID)
	require.NoError(t, err)
	require.NotNil(t, ownerMember, "Owner should be added as a member")
	assert.Equal(t, "owner", ownerMember.Role)
}

func TestGroupRepository_Create_UniqueConstraint(t *testing.T) {
	groupRepo, _, userRepo := setupTestGroups(t)
	owner1 := createTestUserForGroup(t, userRepo, "uniqueOwner1")
	owner2 := createTestUserForGroup(t, userRepo, "uniqueOwner2")
	groupName := "Unique Constraint Test Group"

	// 1. Create first group by owner1 (should succeed)
	group1 := &model.Group{
		Name:    groupName,
		OwnerID: owner1.ID,
	}
	err := groupRepo.Create(group1)
	require.NoError(t, err, "Creating the first group should succeed")

	// 2. Try creating group with same name by SAME owner (should fail)
	group2 := &model.Group{
		Name:    groupName,
		OwnerID: owner1.ID, // Same owner
	}
	err = groupRepo.Create(group2)
	require.Error(t, err, "Creating group with same name by same owner should fail")
	// Check if the error is a duplicate entry error (specific error might depend on DB driver)
	// For now, just checking for any error is sufficient for basic test
	t.Logf("Received expected error when creating duplicate group for same owner: %v", err)

	// 3. Try creating group with same name by DIFFERENT owner (should succeed)
	group3 := &model.Group{
		Name:    groupName,
		OwnerID: owner2.ID, // Different owner
	}
	err = groupRepo.Create(group3)
	require.NoError(t, err, "Creating group with same name by different owner should succeed")
	assert.True(t, group3.ID > 0)
	assert.NotEqual(t, group1.ID, group3.ID) // Ensure different groups were created

	// Verify both groups exist
	foundGroup1, _ := groupRepo.FindByOwnerAndName(owner1.ID, groupName)
	foundGroup3, _ := groupRepo.FindByOwnerAndName(owner2.ID, groupName)
	assert.NotNil(t, foundGroup1)
	assert.NotNil(t, foundGroup3)
}

func TestGroupRepository_FindByID(t *testing.T) {
	groupRepo, groupMemberRepo, userRepo := setupTestGroups(t)
	owner := createTestUserForGroup(t, userRepo, "findByIdOwner")
	memberUser := createTestUserForGroup(t, userRepo, "findByIdMember")

	// Create group
	group := &model.Group{Name: "FindByID Test", OwnerID: owner.ID}
	err := groupRepo.Create(group)
	require.NoError(t, err)

	// Add another member
	err = groupMemberRepo.AddMember(group.ID, memberUser.ID, "member")
	require.NoError(t, err)

	// Find the group
	foundGroup, err := groupRepo.FindByID(group.ID)
	require.NoError(t, err)
	require.NotNil(t, foundGroup)

	// Assert basic info
	assert.Equal(t, group.Name, foundGroup.Name)
	assert.Equal(t, owner.ID, foundGroup.OwnerID)

	// Assert preloaded Owner
	require.NotNil(t, foundGroup.Owner)
	assert.Equal(t, owner.Username, foundGroup.Owner.Username)

	// Assert preloaded Members and their Users
	// NOTE: The Preload in FindByID was `Preload("Member").Preload("Member.User")`
	// It should likely be `Preload("Members").Preload("Members.User")` (plural)
	// Assuming the code in group_repository.go is corrected to use "Members"
	require.Len(t, foundGroup.Members, 2, "Should preload 2 members (owner + added member)")

	foundOwnerMember := false
	foundOtherMember := false
	for _, m := range foundGroup.Members {
		require.NotNil(t, m.User, "Member's User should be preloaded")
		if m.UserID == owner.ID {
			assert.Equal(t, "owner", m.Role)
			assert.Equal(t, owner.Username, m.User.Username)
			foundOwnerMember = true
		} else if m.UserID == memberUser.ID {
			assert.Equal(t, "member", m.Role)
			assert.Equal(t, memberUser.Username, m.User.Username)
			foundOtherMember = true
		}
	}
	assert.True(t, foundOwnerMember, "Owner member not found in preloaded members")
	assert.True(t, foundOtherMember, "Other member not found in preloaded members")

	// Test finding non-existent group
	notFoundGroup, err := groupRepo.FindByID(99999)
	assert.NoError(t, err) // FindByID should return nil, nil for not found
	assert.Nil(t, notFoundGroup)
}

func TestGroupRepository_FindUserGroups(t *testing.T) {
	groupRepo, _, userRepo := setupTestGroups(t)
	user1 := createTestUserForGroup(t, userRepo, "userGroups1")
	user2 := createTestUserForGroup(t, userRepo, "userGroups2")

	// Create groups
	group1 := &model.Group{Name: "User1 Group A", OwnerID: user1.ID}  // user1 is owner
	group2 := &model.Group{Name: "User2 Group B", OwnerID: user2.ID}  // user2 is owner
	group3 := &model.Group{Name: "Shared Group C", OwnerID: user1.ID} // user1 is owner

	require.NoError(t, groupRepo.Create(group1))
	require.NoError(t, groupRepo.Create(group2))
	require.NoError(t, groupRepo.Create(group3))

	// Add user2 to group3
	groupMemberRepo := NewGroupMemberRepository() // Need this repo
	require.NoError(t, groupMemberRepo.AddMember(group3.ID, user2.ID, "member"))

	// Find groups for user1
	groupsUser1, err := groupRepo.FindUserGroups(user1.ID)
	require.NoError(t, err)
	// User1 is owner of group1 and group3
	assert.Len(t, groupsUser1, 2)
	groupNames1 := make(map[string]bool)
	for _, g := range groupsUser1 {
		groupNames1[g.Name] = true
		assert.NotNil(t, g.Owner, "Owner should be preloaded for user groups") // Check preload
	}
	assert.True(t, groupNames1["User1 Group A"])
	assert.True(t, groupNames1["Shared Group C"])

	// Find groups for user2
	groupsUser2, err := groupRepo.FindUserGroups(user2.ID)
	require.NoError(t, err)
	// User2 is owner of group2 and member of group3
	assert.Len(t, groupsUser2, 2)
	groupNames2 := make(map[string]bool)
	for _, g := range groupsUser2 {
		groupNames2[g.Name] = true
		assert.NotNil(t, g.Owner, "Owner should be preloaded for user groups") // Check preload
	}
	assert.True(t, groupNames2["User2 Group B"])
	assert.True(t, groupNames2["Shared Group C"])

	// Find groups for a user with no groups
	user3 := createTestUserForGroup(t, userRepo, "userGroups3")
	groupsUser3, err := groupRepo.FindUserGroups(user3.ID)
	require.NoError(t, err)
	assert.Empty(t, groupsUser3)
}

func TestGroupRepository_FindByName(t *testing.T) {
	groupRepo, _, userRepo := setupTestGroups(t)
	owner1 := createTestUserForGroup(t, userRepo, "findNameOwner1")
	owner2 := createTestUserForGroup(t, userRepo, "findNameOwner2")
	targetName := "Searchable Group Name"

	// Create groups with the same name by different owners
	group1 := &model.Group{Name: targetName, OwnerID: owner1.ID}
	group2 := &model.Group{Name: targetName, OwnerID: owner2.ID}
	group3 := &model.Group{Name: "Another Group", OwnerID: owner1.ID}

	require.NoError(t, groupRepo.Create(group1))
	require.NoError(t, groupRepo.Create(group2))
	require.NoError(t, groupRepo.Create(group3))

	// Find by the target name
	foundGroups, err := groupRepo.FindByName(targetName)
	require.NoError(t, err)
	// Should find both groups with that name
	assert.Len(t, foundGroups, 2)

	foundIDs := make(map[uint]bool)
	for _, g := range foundGroups {
		assert.Equal(t, targetName, g.Name)
		foundIDs[g.ID] = true
	}
	assert.True(t, foundIDs[group1.ID])
	assert.True(t, foundIDs[group2.ID])

	// Find by a name that doesn't exist
	notFoundGroups, err := groupRepo.FindByName("NonExistentGroupName")
	require.NoError(t, err) // Should return nil, nil for not found
	assert.Empty(t, notFoundGroups)
}

func TestGroupRepository_FindByOwnerAndName(t *testing.T) {
	groupRepo, _, userRepo := setupTestGroups(t)
	owner1 := createTestUserForGroup(t, userRepo, "findOwnerName1")
	owner2 := createTestUserForGroup(t, userRepo, "findOwnerName2")
	targetName := "Specific Group Name"

	// Create groups
	group1 := &model.Group{Name: targetName, OwnerID: owner1.ID}
	group2 := &model.Group{Name: targetName, OwnerID: owner2.ID} // Same name, different owner

	require.NoError(t, groupRepo.Create(group1))
	require.NoError(t, groupRepo.Create(group2))

	// Find group1 specifically
	found1, err := groupRepo.FindByOwnerAndName(owner1.ID, targetName)
	require.NoError(t, err)
	require.NotNil(t, found1)
	assert.Equal(t, group1.ID, found1.ID)
	assert.Equal(t, owner1.ID, found1.OwnerID)
	assert.Equal(t, targetName, found1.Name)

	// Find group2 specifically
	found2, err := groupRepo.FindByOwnerAndName(owner2.ID, targetName)
	require.NoError(t, err)
	require.NotNil(t, found2)
	assert.Equal(t, group2.ID, found2.ID)
	assert.Equal(t, owner2.ID, found2.OwnerID)
	assert.Equal(t, targetName, found2.Name)

	// Try finding with correct owner but wrong name
	notFound1, err := groupRepo.FindByOwnerAndName(owner1.ID, "Wrong Name")
	require.NoError(t, err) // Should return nil, nil for not found
	assert.Nil(t, notFound1)

	// Try finding with correct name but wrong owner
	notFound2, err := groupRepo.FindByOwnerAndName(99999, targetName) // Non-existent owner ID
	require.NoError(t, err)                                           // Should return nil, nil for not found
	assert.Nil(t, notFound2)
}
