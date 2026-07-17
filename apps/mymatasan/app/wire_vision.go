package app

import (
	"fmt"
	"os"
	"path/filepath"

	mmconfig "github.com/mysayasan/kopiv2/apps/mymatasan/config"

	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/vision"
)

// detectorModelPaths are the pointer files the Python YOLO worker reads to find the
// models it should load, plus the resolved worker-script arguments.
//
// It exists to turn two invisible dependencies into visible ones:
//
//  1. The detector's script arguments used to be resolved by MUTATING deps.Config in
//     place, and three later constructors silently depended on that write having already
//     happened. Move that one line and training resolves the wrong worker script — no
//     compile error, no failing test. Now the resolved args are a value that consumers
//     take as a parameter, so the compiler enforces the ordering.
//
//  2. The four model-pointer paths were published straight into the process environment
//     with os.Setenv, which is how Go tells the Python worker where the models are. That
//     is a process-global channel: invisible to the type system, untestable in parallel,
//     and it makes two instances in one process impossible.
//
// The env publication still happens (see PublishToProcessEnv) because several Python
// spawn sites — the vision tool, the anomaly checker, the training runner — inherit the
// process environment rather than being handed the paths. Removing it means threading
// this value into each of them, which is a follow-up (Tier 2 phase D3), not something to
// bury inside a mechanical refactor. What changes here is that the paths are a TYPE with
// one publication point, instead of four bare Setenv calls in the middle of wiring.
type detectorModelPaths struct {
	// TrainingDir is the root the pointer files live in.
	TrainingDir string
	// ActiveModelFile points at the model the live detector should load. Rewritten when a
	// trained or imported model is activated, which is how a hot-swap works.
	ActiveModelFile string
	// StockModelFile points at the shipped stock model.
	StockModelFile string
	// LPRModelFile points at the license-plate model.
	LPRModelFile string
	// AnomalyManifestFile lists activated Teach anomaly skills; the worker scores the
	// listed cameras against their normal-memory banks on every frame.
	AnomalyManifestFile string
	// FacesGalleryFile is the enrolled face gallery the worker matches live faces against.
	FacesGalleryFile string
	// FaceYunetFile / FaceSfaceFile are the YuNet detector + SFace embedder .onnx models.
	FaceYunetFile string
	FaceSfaceFile string
	// FacesWorkerScript is the one-shot enrollment worker (faces_worker.py), next to the detector.
	FacesWorkerScript string
	// DetectorArgs are the worker-script arguments, resolved to absolute paths against
	// HomeDir (see resolveDetectorScriptArgs).
	DetectorArgs []string
}

// resolveDetectorModelPaths resolves the worker script and the model pointer files.
//
// The script is resolved against the app HomeDir so it is found regardless of the process
// working directory: a dev run from the repo root, or the staged bin/ bundle (where the
// script sits in <HomeDir>/ai and a repo-root-relative config path would otherwise double
// up as <bin>/apps/mymatasan/ai/...).
func resolveDetectorModelPaths(deps apphost.Dependencies, appCfg *mmconfig.Config) detectorModelPaths {
	trainingDir := trainingDataDir(appCfg)
	abs := func(name string) string {
		p, _ := filepath.Abs(filepath.Join(trainingDir, name))
		return p
	}
	args := resolveDetectorScriptArgs(deps.HomeDir, appCfg.Vision.Detector.Args)
	// The face .onnx models live next to the worker script (ai/ in dev, bin/ai/ staged), like the
	// stock YOLO weights, so they resolve to wherever the detector script resolved to.
	faceDir := trainingDir
	if len(args) > 0 {
		faceDir = filepath.Dir(args[0])
	}
	return detectorModelPaths{
		TrainingDir:         trainingDir,
		ActiveModelFile:     abs("active_model.txt"),
		StockModelFile:      abs("stock_model.txt"),
		LPRModelFile:        abs("lpr_model.txt"),
		AnomalyManifestFile: abs("anomaly_models.json"),
		FacesGalleryFile:    abs("faces_gallery.json"),
		FaceYunetFile:       filepath.Join(faceDir, "face_detection_yunet_2023mar.onnx"),
		FaceSfaceFile:       filepath.Join(faceDir, "face_recognition_sface_2021dec.onnx"),
		FacesWorkerScript:   filepath.Join(faceDir, "faces_worker.py"),
		DetectorArgs:        args,
	}
}

// PublishToProcessEnv exports the model pointers into the process environment, where the
// Python workers read them.
//
// This is the one place that touches the global environment. It is called once, from the
// composition root, and its callers take the typed paths instead — see the type comment
// for why the env channel still exists and how it goes away.
func (p detectorModelPaths) PublishToProcessEnv() {
	_ = os.Setenv("MYMATASAN_ACTIVE_MODEL_FILE", p.ActiveModelFile)
	_ = os.Setenv("MYMATASAN_STOCK_MODEL_FILE", p.StockModelFile)
	_ = os.Setenv("MYMATASAN_LPR_MODEL_FILE", p.LPRModelFile)
	_ = os.Setenv("MYMATASAN_ANOMALY_FILE", p.AnomalyManifestFile)
	_ = os.Setenv("MYMATASAN_FACES_FILE", p.FacesGalleryFile)
	_ = os.Setenv("MYMATASAN_FACE_YUNET", p.FaceYunetFile)
	_ = os.Setenv("MYMATASAN_FACE_SFACE", p.FaceSfaceFile)
}

// Env renders the model pointers as KEY=VALUE pairs, for a spawn site that hands the
// child an explicit environment rather than relying on inheritance.
func (p detectorModelPaths) Env() []string {
	return []string{
		fmt.Sprintf("MYMATASAN_ACTIVE_MODEL_FILE=%s", p.ActiveModelFile),
		fmt.Sprintf("MYMATASAN_STOCK_MODEL_FILE=%s", p.StockModelFile),
		fmt.Sprintf("MYMATASAN_LPR_MODEL_FILE=%s", p.LPRModelFile),
		fmt.Sprintf("MYMATASAN_ANOMALY_FILE=%s", p.AnomalyManifestFile),
		fmt.Sprintf("MYMATASAN_FACES_FILE=%s", p.FacesGalleryFile),
		fmt.Sprintf("MYMATASAN_FACE_YUNET=%s", p.FaceYunetFile),
		fmt.Sprintf("MYMATASAN_FACE_SFACE=%s", p.FaceSfaceFile),
	}
}

// detectorConfigWithArgs returns the configured detector with its script arguments
// replaced by the resolved ones. It takes a copy — the shared config model must NOT be
// mutated, because a later reader has no way to know whether the write has happened yet.
func detectorConfigWithArgs(cfg mmconfig.VisionDetectorConfigModel, args []string) mmconfig.VisionDetectorConfigModel {
	cfg.Args = args
	return cfg
}

// buildObjectDetectorBackend builds the shared object-detection backend used by both the
// live vision monitor and the training auto-labeler. A nil backend is not fatal: it
// disables auto-label and custom models, and the caller says so in the log.
func buildObjectDetectorBackend(deps apphost.Dependencies, appCfg *mmconfig.Config, paths detectorModelPaths) vision.ObjectDetector {
	backend, err := buildTrainingObjectDetector(detectorConfigWithArgs(appCfg.Vision.Detector, paths.DetectorArgs))
	if err != nil {
		deps.Logger.Warnf("mymatasan.vision", "object detector backend unavailable (%v); auto-label and custom models are disabled", err)
		return nil
	}
	return backend
}

// teachDetectorConfig builds the Teach wizard's detector settings from the resolved paths,
// so it cannot silently pick up unresolved script arguments.
func teachDetectorConfig(appCfg *mmconfig.Config, paths detectorModelPaths) services.TeachDetectorConfig {
	return services.TeachDetectorConfig{
		Command:         appCfg.Vision.Detector.Command,
		Args:            paths.DetectorArgs,
		TimeoutMs:       appCfg.Vision.Detector.TimeoutMs,
		AnomalyManifest: paths.AnomalyManifestFile,
	}
}
