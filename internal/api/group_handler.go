package api

import (
	"errors"
	"go-chat-room/internal/service"
	"go-chat-room/pkg/logger"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type GroupHandler struct {
	chatService *service.ChatService
}

func NewGroupHandler(chatService *service.ChatService) *GroupHandler {
	return &GroupHandler{
		chatService: chatService,
	}
}

func (h *GroupHandler) CreateGroup(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	var req service.CreateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.L.Warn("Failed to bind CreateGroup request", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	group, err := h.chatService.CreateGroup(userID, req)
	if err != nil {
		logger.L.Error("Error creating group via service", zap.Error(err), zap.Uint("ownerID", userID))
		if err.Error() == "you already have a group with this name" {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create group"})
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Group created successfully",
		"group": gin.H{
			"id":         group.ID,
			"name":       group.Name,
			"owner_id":   group.OwnerID,
			"created_at": group.CreatedAt,
		},
	})
}

func (h *GroupHandler) GetUserGroups(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	groups, err := h.chatService.GetUserGroups(userID)
	if err != nil {
		logger.L.Error("Error getting user groups from service", zap.Error(err), zap.Uint("userID", userID))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve groups"})
		return
	}

	responseGroups := make([]gin.H, 0, len(groups))
	for _, g := range groups {
		responseGroups = append(responseGroups, gin.H{
			"id":             g.ID,
			"name":           g.Name,
			"owner_id":       g.OwnerID,
			"created_at":     g.CreatedAt,
			"owner_username": g.Owner.Username,
		})
	}

	c.JSON(http.StatusOK, gin.H{"groups": responseGroups})
}

func (h *GroupHandler) GetGroupInfo(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}
	groupID, ok := getGroupIDFromParam(c)
	if !ok {
		return
	}

	group, err := h.chatService.GetGroupInfo(groupID, userID)
	if err != nil {
		logger.L.Warn("Error getting group info from service", zap.Error(err), zap.Uint("groupID", groupID), zap.Uint("requesterID", userID))
		if errors.Is(err, gorm.ErrRecordNotFound) || err.Error() == "group not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Group not found"})
		} else if err.Error() == "you are not a member of this group" {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve group info"})
		}
		return
	}

	membersResponse := make([]gin.H, 0, len(group.Members))
	for _, m := range group.Members {
		membersResponse = append(membersResponse, gin.H{
			"user_id":  m.UserID,
			"username": m.User.Username,
			"avatar":   m.User.Avatar,
			"role":     m.Role,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"id":         group.ID,
		"name":       group.Name,
		"owner_id":   group.OwnerID,
		"created_at": group.CreatedAt,
		"owner": gin.H{
			"user_id":  group.Owner.ID,
			"username": group.Owner.Username,
			"avatar":   group.Owner.Avatar,
		},
		"members": membersResponse,
	})
}

func (h *GroupHandler) AddGroupMember(c *gin.Context) {
	requesterID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}
	groupID, ok := getGroupIDFromParam(c)
	if !ok {
		return
	}

	var req service.AddGroupMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.L.Warn("Failed to bind AddGroupMember request", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: user_id is required"})
		return
	}

	err := h.chatService.AddGroupMember(groupID, req.UserID, requesterID)
	if err != nil {
		logger.L.Warn("Error adding group member via service", zap.Error(err), zap.Uint("groupID", groupID), zap.Uint("targetUserID", req.UserID), zap.Uint("requesterID", requesterID))
		// 根据错误类型返回不同的状态码
		errMsg := err.Error()
		switch {
		case errMsg == "requester is not a member or group not found",
			errMsg == "target user does not exist",
			errMsg == "group not found": // 可能由 FindMember 返回
			c.JSON(http.StatusNotFound, gin.H{"error": errMsg})
		case errMsg == "only the group owner or admin can add members":
			c.JSON(http.StatusForbidden, gin.H{"error": errMsg})
		case errMsg == "user is already a member of this group":
			c.JSON(http.StatusConflict, gin.H{"error": errMsg})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add member"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User added to group successfully"})
}

func (h *GroupHandler) RemoveGroupMember(c *gin.Context) {
	requesterID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}
	groupID, ok := getGroupIDFromParam(c)
	if !ok {
		return
	}
	targetUserIDStr := c.Param("user_id")
	targetUserID64, err := strconv.ParseUint(targetUserIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid target user_id parameter"})
		return
	}
	targetUserID := uint(targetUserID64)

	err = h.chatService.RemoveGroupMember(groupID, targetUserID, requesterID)
	if err != nil {
		logger.L.Warn("Error removing group member via service", zap.Error(err), zap.Uint("groupID", groupID), zap.Uint("targetUserID", targetUserID), zap.Uint("requesterID", requesterID))
		errMsg := err.Error()
		switch {
		case errMsg == "requester is not a member or group not found",
			errMsg == "target user is not a member or group not found",
			errMsg == "group not found":
			c.JSON(http.StatusNotFound, gin.H{"error": errMsg})
		case errMsg == "only the group owner or admin can remove other members",
			errMsg == "group owner cannot leave the group (consider transferring ownership or deleting the group)",
			errMsg == "group admin cannot remove other admins",
			errMsg == "cannot remove the group owner":
			c.JSON(http.StatusForbidden, gin.H{"error": errMsg})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove member"})
		}
		return
	}

	message := "User removed from group successfully"
	if requesterID == targetUserID {
		message = "You have left the group successfully"
	}
	c.JSON(http.StatusOK, gin.H{"message": message})
}

func (h *GroupHandler) GetGroupChatHistory(c *gin.Context) {
	requesterID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}
	groupID, ok := getGroupIDFromParam(c)
	if !ok {
		return
	}
	limit, offset, ok := getPaginationParams(c)
	if !ok {
		return
	}

	messages, err := h.chatService.GetGroupChatHistory(groupID, requesterID, limit, offset)
	if err != nil {
        logger.L.Error("Error getting group chat history from service", zap.Error(err), zap.Uint("groupID", groupID), zap.Uint("requesterID", requesterID))
        if err.Error() == "you are not a member of this group" {
            c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
        } else {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve group chat history"})
        }
        return
	}

	c.JSON(http.StatusOK, gin.H{"message": messages})
}

func getUserIDFromContext(c *gin.Context) (uint, bool) {
	userIDValue, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return 0, false
	}
	userID, ok := userIDValue.(uint)
	if !ok {
		logger.L.Error("Invalid userID type in context", zap.Any("userIDValue", userIDValue))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user ID in context"})
		return 0, false
	}
	return userID, true
}

func getGroupIDFromParam(c *gin.Context) (uint, bool) {
	groupIDStr := c.Param("group_id")
	groupID64, err := strconv.ParseUint(groupIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid group_id parameter"})
		return 0, false
	}
	return uint(groupID64), true
}

func getPaginationParams(c *gin.Context) (limit, offset int, ok bool) {
	var err error
	limit, err = strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, err = strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		offset = 0
	}
	return limit, offset, true
}
