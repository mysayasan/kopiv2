package app

import (
	appentities "github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// repos are the app's persistence handles, one per entity.
//
// They were previously constructed in three clumps scattered through the composition
// root, which made the app's actual data surface impossible to see at a glance. Gathering
// them here means the set of things mymatasan persists is one screen, and it lines up
// with module.Entities() — the list the schema is derived from.
type repos struct {
	Camera            dbsql.IGenericRepo[appentities.Camera]
	CameraOnvif       dbsql.IGenericRepo[appentities.CameraOnvif]
	DetectionRule     dbsql.IGenericRepo[appentities.DetectionRule]
	DetectionClass    dbsql.IGenericRepo[appentities.DetectionClass]
	AlertEvent        dbsql.IGenericRepo[appentities.AlertEvent]
	ObjectObservation dbsql.IGenericRepo[appentities.ObjectObservation]
	RecordingConfig   dbsql.IGenericRepo[appentities.RecordingConfig]
	RecordingSegment  dbsql.IGenericRepo[appentities.RecordingSegment]
	TrainingDataset   dbsql.IGenericRepo[appentities.TrainingDataset]
	TrainingImage     dbsql.IGenericRepo[appentities.TrainingImage]
	TrainingModel     dbsql.IGenericRepo[appentities.TrainingModel]
	TeachSkill        dbsql.IGenericRepo[appentities.TeachSkill]
	FacePerson        dbsql.IGenericRepo[appentities.FacePerson]
	FaceEmbedding     dbsql.IGenericRepo[appentities.FaceEmbedding]
	RuntimeSetting    dbsql.IGenericRepo[appentities.RuntimeSetting]
	LocalUser         dbsql.IGenericRepo[appentities.LocalUser]

	// Shared-domain tables, owned by domain/ rather than by this app.
	Notification       dbsql.IGenericRepo[sharedentities.Notification]
	NotificationRollup dbsql.IGenericRepo[sharedentities.NotificationRollup]
}

// newRepos builds every repository against the bootstrapped database.
func newRepos(db dbsql.IDbCrud) repos {
	return repos{
		Camera:            dbsql.NewGenericRepo[appentities.Camera](db),
		CameraOnvif:       dbsql.NewGenericRepo[appentities.CameraOnvif](db),
		DetectionRule:     dbsql.NewGenericRepo[appentities.DetectionRule](db),
		DetectionClass:    dbsql.NewGenericRepo[appentities.DetectionClass](db),
		AlertEvent:        dbsql.NewGenericRepo[appentities.AlertEvent](db),
		ObjectObservation: dbsql.NewGenericRepo[appentities.ObjectObservation](db),
		RecordingConfig:   dbsql.NewGenericRepo[appentities.RecordingConfig](db),
		RecordingSegment:  dbsql.NewGenericRepo[appentities.RecordingSegment](db),
		TrainingDataset:   dbsql.NewGenericRepo[appentities.TrainingDataset](db),
		TrainingImage:     dbsql.NewGenericRepo[appentities.TrainingImage](db),
		TrainingModel:     dbsql.NewGenericRepo[appentities.TrainingModel](db),
		TeachSkill:        dbsql.NewGenericRepo[appentities.TeachSkill](db),
		FacePerson:        dbsql.NewGenericRepo[appentities.FacePerson](db),
		FaceEmbedding:     dbsql.NewGenericRepo[appentities.FaceEmbedding](db),
		RuntimeSetting:    dbsql.NewGenericRepo[appentities.RuntimeSetting](db),
		LocalUser:         dbsql.NewGenericRepo[appentities.LocalUser](db),

		Notification:       dbsql.NewGenericRepo[sharedentities.Notification](db),
		NotificationRollup: dbsql.NewGenericRepo[sharedentities.NotificationRollup](db),
	}
}
