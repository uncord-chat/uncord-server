package api

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/audit"
	"github.com/uncord-chat/uncord-server/internal/category"
	"github.com/uncord-chat/uncord-server/internal/httputil"
)

// CategoryHandler serves category endpoints.
type CategoryHandler struct {
	categories    category.Repository
	maxCategories int
	auditLogger   *audit.Logger
	log           zerolog.Logger
}

// NewCategoryHandler creates a new category handler.
func NewCategoryHandler(categories category.Repository, maxCategories int, auditLogger *audit.Logger, logger zerolog.Logger) *CategoryHandler {
	return &CategoryHandler{categories: categories, maxCategories: maxCategories, auditLogger: auditLogger, log: logger}
}

// ListCategories handles GET /api/v1/server/categories.
func (h *CategoryHandler) ListCategories(c fiber.Ctx) error {
	cats, err := h.categories.List(c)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "category").Msg("list categories failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	result := make([]models.Category, len(cats))
	for i := range cats {
		result[i] = toCategoryModel(&cats[i])
	}
	return httputil.Success(c, result)
}

// CreateCategory handles POST /api/v1/server/categories.
func (h *CategoryHandler) CreateCategory(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	var body models.CreateCategoryRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	name, err := category.ValidateNameRequired(body.Name)
	if err != nil {
		return h.mapCategoryError(c, err)
	}

	cat, err := h.categories.Create(c, category.CreateParams{Name: name}, h.maxCategories)
	if err != nil {
		return h.mapCategoryError(c, err)
	}

	if h.auditLogger != nil {
		go h.auditLogger.Record(context.Background(), audit.Entry{
			ActorID: audit.UUIDPtr(userID), Action: audit.CategoryCreate,
			TargetType: audit.Ptr("category"), TargetID: audit.UUIDPtr(cat.ID),
		})
	}

	return httputil.SuccessStatus(c, fiber.StatusCreated, toCategoryModel(cat))
}

// UpdateCategory handles PATCH /api/v1/categories/:categoryID.
func (h *CategoryHandler) UpdateCategory(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	id, err := uuid.Parse(c.Params("categoryID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid category ID format")
	}

	var body models.UpdateCategoryRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	if err := category.ValidateName(body.Name); err != nil {
		return h.mapCategoryError(c, err)
	}
	if err := category.ValidatePosition(body.Position); err != nil {
		return h.mapCategoryError(c, err)
	}

	cat, err := h.categories.Update(c, id, category.UpdateParams{
		Name:     body.Name,
		Position: body.Position,
	})
	if err != nil {
		return h.mapCategoryError(c, err)
	}

	if h.auditLogger != nil {
		go h.auditLogger.Record(context.Background(), audit.Entry{
			ActorID: audit.UUIDPtr(userID), Action: audit.CategoryUpdate,
			TargetType: audit.Ptr("category"), TargetID: audit.UUIDPtr(id),
		})
	}

	return httputil.Success(c, toCategoryModel(cat))
}

// DeleteCategory handles DELETE /api/v1/categories/:categoryID.
func (h *CategoryHandler) DeleteCategory(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	id, err := uuid.Parse(c.Params("categoryID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid category ID format")
	}

	if err := h.categories.Delete(c, id); err != nil {
		return h.mapCategoryError(c, err)
	}

	if h.auditLogger != nil {
		go h.auditLogger.Record(context.Background(), audit.Entry{
			ActorID: audit.UUIDPtr(userID), Action: audit.CategoryDelete,
			TargetType: audit.Ptr("category"), TargetID: audit.UUIDPtr(id),
		})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// toCategoryModel converts the internal category to the protocol response type.
func toCategoryModel(cat *category.Category) models.Category {
	return models.Category{
		ID:        cat.ID.String(),
		Name:      cat.Name,
		Position:  cat.Position,
		CreatedAt: cat.CreatedAt.Format(time.RFC3339),
		UpdatedAt: cat.UpdatedAt.Format(time.RFC3339),
	}
}

// mapCategoryError converts category-layer errors to appropriate HTTP responses.
func (h *CategoryHandler) mapCategoryError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, category.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownCategory, "Category not found")
	case errors.Is(err, category.ErrNameLength):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, category.ErrInvalidPosition):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, category.ErrAlreadyExists):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.AlreadyExists, err.Error())
	case errors.Is(err, category.ErrMaxCategoriesReached):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.MaxCategoriesReached, err.Error())
	default:
		h.log.Error().Err(err).Str("handler", "category").Msg("unhandled category service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
