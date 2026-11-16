package catalog

import (
	"context"
	"database/sql"
	"errors"
)

type Service struct {
	repository *Repository
}

func NewService(repository *Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Available() bool {
	return s != nil && s.repository.available()
}

func (s *Service) ListMaterials(ctx context.Context, query listQuery) (materialCatalogResponse, error) {
	if !s.Available() {
		return materialCatalogResponse{}, ErrServiceUnavailable
	}
	materials, total, err := s.repository.ListMaterials(ctx, query)
	if err != nil {
		return materialCatalogResponse{}, err
	}
	subjects, err := s.repository.ListPublishedSubjects(ctx)
	if err != nil {
		return materialCatalogResponse{}, err
	}
	items := make([]materialListItem, 0, len(materials))
	for _, material := range materials {
		items = append(items, toMaterialListItem(material))
	}
	filterSubjects := make([]subjectResponse, 0, len(subjects))
	for _, subject := range subjects {
		filterSubjects = append(filterSubjects, toSubjectResponse(subject))
	}
	return materialCatalogResponse{
		Items:      items,
		Pagination: pagination{Page: query.Page, PageSize: query.PageSize, Total: total},
		Filters: listFilters{
			EducationLevels: []filterOption{
				{Value: "SD", Label: "SD"}, {Value: "SMP", Label: "SMP"}, {Value: "SMA", Label: "SMA"},
			},
			AccessTiers: []filterOption{
				{Value: "FREE", Label: "Gratis"}, {Value: "PREMIUM", Label: "Premium"},
			},
			Sorts: []filterOption{
				{Value: "relevan", Label: "Paling Relevan"}, {Value: "terbaru", Label: "Terbaru Ditambahkan"},
				{Value: "populer", Label: "Modul Terpopuler"}, {Value: "latihan", Label: "Jumlah Latihan Terbanyak"},
			},
			Subjects: filterSubjects,
		},
	}, nil
}

func (s *Service) GetMaterial(ctx context.Context, slug, userID, role string, authenticated bool) (materialDetail, error) {
	if !s.Available() {
		return materialDetail{}, ErrServiceUnavailable
	}
	material, err := s.repository.FindMaterialBySlug(ctx, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return materialDetail{}, ErrMaterialNotFound
	}
	if err != nil {
		return materialDetail{}, err
	}
	if material.AccessTier == "PREMIUM" {
		if !authenticated {
			return materialDetail{}, ErrPremiumAccessRequired
		}
		if role != "ADMIN" {
			hasGrant, err := s.repository.HasMaterialGrant(ctx, userID, material.ID)
			if err != nil {
				return materialDetail{}, err
			}
			if !hasGrant {
				return materialDetail{}, ErrPremiumAccessRequired
			}
		}
	}
	material.Outcomes, err = s.repository.ListOutcomes(ctx, material.ID)
	if err != nil {
		return materialDetail{}, err
	}
	material.Chapters, err = s.repository.ListChapters(ctx, material.ID)
	if err != nil {
		return materialDetail{}, err
	}
	return toMaterialDetail(material), nil
}

func (s *Service) ListPackages(ctx context.Context, query listQuery) (packageCatalogResponse, error) {
	if !s.Available() {
		return packageCatalogResponse{}, ErrServiceUnavailable
	}
	packages, total, err := s.repository.ListPackages(ctx, query)
	if err != nil {
		return packageCatalogResponse{}, err
	}
	items := make([]packageListItem, 0, len(packages))
	for _, assessmentPackage := range packages {
		items = append(items, toPackageListItem(assessmentPackage))
	}
	return packageCatalogResponse{
		Items:      items,
		Pagination: pagination{Page: query.Page, PageSize: query.PageSize, Total: total},
	}, nil
}

func toSubjectResponse(subject Subject) subjectResponse {
	return subjectResponse{ID: subject.ID, Slug: subject.Slug, Name: subject.Name}
}

func toMaterialListItem(material Material) materialListItem {
	return materialListItem{
		ID: material.ID, Slug: material.Slug, Title: material.Title, Description: material.Description,
		Subject: toSubjectResponse(material.Subject), EducationLevel: material.EducationLevel, Grade: material.Grade,
		Curriculum: material.Curriculum, Category: material.Category, AccessTier: material.AccessTier,
		PriceAmount: material.PriceAmount, Currency: material.Currency, ImageURL: material.ImageURL,
		LessonCount: material.LessonCount, ExerciseCount: material.ExerciseCount, LearnerCount: material.LearnerCount,
		PublishedAt: material.PublishedAt,
	}
}

func toMaterialDetail(material Material) materialDetail {
	var instructor *instructorResponse
	if material.Instructor != nil {
		instructor = &instructorResponse{
			ID: material.Instructor.ID, Name: material.Instructor.Name, Role: material.Instructor.Role,
			Description: material.Instructor.Description, ImageURL: material.Instructor.ImageURL,
			Expertise: material.Instructor.Expertise,
		}
	}
	outcomes := make([]outcomeResponse, 0, len(material.Outcomes))
	for _, outcome := range material.Outcomes {
		outcomes = append(outcomes, outcomeResponse{
			ID: outcome.ID, Position: outcome.Position, Title: outcome.Title, Description: outcome.Description,
		})
	}
	chapters := make([]chapterResponse, 0, len(material.Chapters))
	for _, chapter := range material.Chapters {
		lessons := make([]lessonResponse, 0, len(chapter.Lessons))
		for _, lesson := range chapter.Lessons {
			lessons = append(lessons, lessonResponse{
				ID: lesson.ID, Code: lesson.Code, Position: lesson.Position, Title: lesson.Title,
				Description: lesson.Description, ContentType: lesson.ContentType, DurationSeconds: lesson.DurationSeconds,
				VideoURL: lesson.VideoURL, PosterURL: lesson.PosterURL, Objective: lesson.Objective, Summary: lesson.Summary,
			})
		}
		chapters = append(chapters, chapterResponse{
			ID: chapter.ID, Position: chapter.Position, Title: chapter.Title,
			Description: chapter.Description, Lessons: lessons,
		})
	}
	return materialDetail{
		ID: material.ID, Slug: material.Slug, Title: material.Title, Description: material.Description,
		Subject: toSubjectResponse(material.Subject), EducationLevel: material.EducationLevel, Grade: material.Grade,
		Curriculum: material.Curriculum, Category: material.Category, AccessTier: material.AccessTier,
		PriceAmount: material.PriceAmount, Currency: material.Currency, ImageURL: material.ImageURL,
		PublishedAt: material.PublishedAt, Instructor: instructor, Outcomes: outcomes, Chapters: chapters,
	}
}

func toPackageListItem(assessmentPackage AssessmentPackage) packageListItem {
	var material *packageMaterial
	if assessmentPackage.Material != nil {
		material = &packageMaterial{
			ID: assessmentPackage.Material.ID, Slug: assessmentPackage.Material.Slug, Title: assessmentPackage.Material.Title,
		}
	}
	return packageListItem{
		ID: assessmentPackage.ID, Slug: assessmentPackage.Slug, Title: assessmentPackage.Title,
		Description: assessmentPackage.Description, Kind: assessmentPackage.Kind,
		Subject: toSubjectResponse(assessmentPackage.Subject), Material: material,
		EducationLevel: assessmentPackage.EducationLevel, Grade: assessmentPackage.Grade,
		AccessTier: assessmentPackage.AccessTier, PriceAmount: assessmentPackage.PriceAmount,
		Difficulty: assessmentPackage.Difficulty, DurationSeconds: assessmentPackage.DurationSeconds,
		QuestionCount: assessmentPackage.QuestionCount, PublishedAt: assessmentPackage.PublishedAt,
	}
}
