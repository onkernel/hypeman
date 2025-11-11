package api

import (
	"context"
	"errors"
	"strings"

	"github.com/onkernel/hypeman/lib/instances"
	"github.com/onkernel/hypeman/lib/logger"
	"github.com/onkernel/hypeman/lib/oapi"
)

// ListInstances lists all instances
func (s *ApiService) ListInstances(ctx context.Context, request oapi.ListInstancesRequestObject) (oapi.ListInstancesResponseObject, error) {
	log := logger.FromContext(ctx)

	domainInsts, err := s.InstanceManager.ListInstances(ctx)
	if err != nil {
		log.Error("failed to list instances", "error", err)
		return oapi.ListInstances500JSONResponse{
			Code:    "internal_error",
			Message: "failed to list instances",
		}, nil
	}

	oapiInsts := make([]oapi.Instance, len(domainInsts))
	for i, inst := range domainInsts {
		oapiInsts[i] = instanceToOAPI(inst)
	}

	return oapi.ListInstances200JSONResponse(oapiInsts), nil
}

// CreateInstance creates and starts a new instance
func (s *ApiService) CreateInstance(ctx context.Context, request oapi.CreateInstanceRequestObject) (oapi.CreateInstanceResponseObject, error) {
	log := logger.FromContext(ctx)

	// Apply defaults
	size := int64(1073741824) // 1GB default
	if request.Body.Size != nil {
		size = *request.Body.Size
	}

	hotplugSize := int64(3221225472) // 3GB default
	if request.Body.HotplugSize != nil {
		hotplugSize = *request.Body.HotplugSize
	}

	vcpus := 2
	if request.Body.Vcpus != nil {
		vcpus = *request.Body.Vcpus
	}

	env := make(map[string]string)
	if request.Body.Env != nil {
		env = *request.Body.Env
	}

	domainReq := instances.CreateInstanceRequest{
		Name:        request.Body.Name,
		Image:       request.Body.Image,
		Size:        size,
		HotplugSize: hotplugSize,
		Vcpus:       vcpus,
		Env:         env,
	}

	inst, err := s.InstanceManager.CreateInstance(ctx, domainReq)
	if err != nil {
		switch {
		case errors.Is(err, instances.ErrImageNotReady):
			return oapi.CreateInstance400JSONResponse{
				Code:    "image_not_ready",
				Message: err.Error(),
			}, nil
		case errors.Is(err, instances.ErrAlreadyExists):
			return oapi.CreateInstance400JSONResponse{
				Code:    "already_exists",
				Message: "instance already exists",
			}, nil
		default:
			log.Error("failed to create instance", "error", err, "image", request.Body.Image)
			return oapi.CreateInstance500JSONResponse{
				Code:    "internal_error",
				Message: "failed to create instance",
			}, nil
		}
	}
	return oapi.CreateInstance201JSONResponse(instanceToOAPI(*inst)), nil
}

// GetInstance gets instance details
func (s *ApiService) GetInstance(ctx context.Context, request oapi.GetInstanceRequestObject) (oapi.GetInstanceResponseObject, error) {
	log := logger.FromContext(ctx)

	inst, err := s.InstanceManager.GetInstance(ctx, request.Id)
	if err != nil {
		switch {
		case errors.Is(err, instances.ErrNotFound):
			return oapi.GetInstance404JSONResponse{
				Code:    "not_found",
				Message: "instance not found",
			}, nil
		default:
			log.Error("failed to get instance", "error", err, "id", request.Id)
			return oapi.GetInstance500JSONResponse{
				Code:    "internal_error",
				Message: "failed to get instance",
			}, nil
		}
	}
	return oapi.GetInstance200JSONResponse(instanceToOAPI(*inst)), nil
}

// DeleteInstance stops and deletes an instance
func (s *ApiService) DeleteInstance(ctx context.Context, request oapi.DeleteInstanceRequestObject) (oapi.DeleteInstanceResponseObject, error) {
	log := logger.FromContext(ctx)

	err := s.InstanceManager.DeleteInstance(ctx, request.Id)
	if err != nil {
		switch {
		case errors.Is(err, instances.ErrNotFound):
			return oapi.DeleteInstance404JSONResponse{
				Code:    "not_found",
				Message: "instance not found",
			}, nil
		default:
			log.Error("failed to delete instance", "error", err, "id", request.Id)
			return oapi.DeleteInstance500JSONResponse{
				Code:    "internal_error",
				Message: "failed to delete instance",
			}, nil
		}
	}
	return oapi.DeleteInstance204Response{}, nil
}

// StandbyInstance puts an instance in standby (pause, snapshot, delete VMM)
func (s *ApiService) StandbyInstance(ctx context.Context, request oapi.StandbyInstanceRequestObject) (oapi.StandbyInstanceResponseObject, error) {
	log := logger.FromContext(ctx)

	inst, err := s.InstanceManager.StandbyInstance(ctx, request.Id)
	if err != nil {
		switch {
		case errors.Is(err, instances.ErrNotFound):
			return oapi.StandbyInstance404JSONResponse{
				Code:    "not_found",
				Message: "instance not found",
			}, nil
		case errors.Is(err, instances.ErrInvalidState):
			return oapi.StandbyInstance409JSONResponse{
				Code:    "invalid_state",
				Message: err.Error(),
			}, nil
		default:
			log.Error("failed to standby instance", "error", err, "id", request.Id)
			return oapi.StandbyInstance500JSONResponse{
				Code:    "internal_error",
				Message: "failed to standby instance",
			}, nil
		}
	}
	return oapi.StandbyInstance200JSONResponse(instanceToOAPI(*inst)), nil
}

// RestoreInstance restores an instance from standby
func (s *ApiService) RestoreInstance(ctx context.Context, request oapi.RestoreInstanceRequestObject) (oapi.RestoreInstanceResponseObject, error) {
	log := logger.FromContext(ctx)

	inst, err := s.InstanceManager.RestoreInstance(ctx, request.Id)
	if err != nil {
		switch {
		case errors.Is(err, instances.ErrNotFound):
			return oapi.RestoreInstance404JSONResponse{
				Code:    "not_found",
				Message: "instance not found",
			}, nil
		case errors.Is(err, instances.ErrInvalidState):
			return oapi.RestoreInstance409JSONResponse{
				Code:    "invalid_state",
				Message: err.Error(),
			}, nil
		default:
			log.Error("failed to restore instance", "error", err, "id", request.Id)
			return oapi.RestoreInstance500JSONResponse{
				Code:    "internal_error",
				Message: "failed to restore instance",
			}, nil
		}
	}
	return oapi.RestoreInstance200JSONResponse(instanceToOAPI(*inst)), nil
}

// GetInstanceLogs streams instance logs
func (s *ApiService) GetInstanceLogs(ctx context.Context, request oapi.GetInstanceLogsRequestObject) (oapi.GetInstanceLogsResponseObject, error) {
	log := logger.FromContext(ctx)

	follow := false
	if request.Params.Follow != nil {
		follow = *request.Params.Follow
	}
	tail := 100
	if request.Params.Tail != nil {
		tail = *request.Params.Tail
	}

	logs, err := s.InstanceManager.GetInstanceLogs(ctx, request.Id, follow, tail)
	if err != nil {
		switch {
		case errors.Is(err, instances.ErrNotFound):
			return oapi.GetInstanceLogs404JSONResponse{
				Code:    "not_found",
				Message: "instance not found",
			}, nil
		default:
			log.Error("failed to get instance logs", "error", err, "id", request.Id)
			return oapi.GetInstanceLogs500JSONResponse{
				Code:    "internal_error",
				Message: "failed to get instance logs",
			}, nil
		}
	}

	return oapi.GetInstanceLogs200TexteventStreamResponse{
		Body:          strings.NewReader(logs),
		ContentLength: int64(len(logs)),
	}, nil
}

// AttachVolume attaches a volume to an instance (not yet implemented)
func (s *ApiService) AttachVolume(ctx context.Context, request oapi.AttachVolumeRequestObject) (oapi.AttachVolumeResponseObject, error) {
	return oapi.AttachVolume500JSONResponse{
		Code:    "not_implemented",
		Message: "volume attachment not yet implemented",
	}, nil
}

// DetachVolume detaches a volume from an instance (not yet implemented)
func (s *ApiService) DetachVolume(ctx context.Context, request oapi.DetachVolumeRequestObject) (oapi.DetachVolumeResponseObject, error) {
	return oapi.DetachVolume500JSONResponse{
		Code:    "not_implemented",
		Message: "volume detachment not yet implemented",
	}, nil
}

// instanceToOAPI converts domain Instance to OAPI Instance
func instanceToOAPI(inst instances.Instance) oapi.Instance {
	oapiInst := oapi.Instance{
		Id:          inst.Id,
		Name:        inst.Name,
		Image:       inst.Image,
		State:       oapi.InstanceState(inst.State),
		Size:        &inst.Size,
		HotplugSize: &inst.HotplugSize,
		Vcpus:       &inst.Vcpus,
		CreatedAt:   inst.CreatedAt,
		StartedAt:   inst.StartedAt,
		StoppedAt:   inst.StoppedAt,
		HasSnapshot: &inst.HasSnapshot,
	}

	if len(inst.Env) > 0 {
		oapiInst.Env = &inst.Env
	}

	return oapiInst
}
