package addon

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/kris-nova/logger"

	api "github.com/weaveworks/eksctl/pkg/apis/eksctl.io/v1alpha5"
	"github.com/weaveworks/eksctl/pkg/utils/tasks"
)

func (a *Manager) DeleteWithPreserve(ctx context.Context, addon *api.Addon) error {
	logger.Info("deleting addon %q and preserving its resources", addon.Name)
	_, err := a.deleteAddon(ctx, addon, true)
	return err
}

func (a *Manager) Delete(ctx context.Context, addon *api.Addon) error {
	logger.Debug("addon: %v", addon)
	logger.Info("deleting addon: %s", addon.Name)
	addonExists, err := a.deleteAddon(ctx, addon, false)
	if err != nil {
		return err
	}
	if addonExists {
		logger.Info("deleted addon: %s", addon.Name)
	}

	deleteAddonIAMTasks, err := NewRemover(a.stackManager).DeleteAddonIAMTasks(ctx, true)
	if err != nil {
		return err
	}
	if deleteAddonIAMTasks.Len() > 0 {
		logger.Info("deleting associated IAM stacks")
		logger.Info(deleteAddonIAMTasks.Describe())
		if errs := deleteAddonIAMTasks.DoAllSync(); len(errs) > 0 {
			var allErrs []string
			for _, err := range errs {
				allErrs = append(allErrs, err.Error())
			}
			return fmt.Errorf(strings.Join(allErrs, "\n"))
		}
		logger.Info("all tasks were completed successfully")
	} else if addonExists {
		logger.Info("no associated IAM stacks found")
	} else {
		return errors.New("could not find addon or associated IAM stack to delete")
	}

	return nil
}

func (a *Manager) deleteAddon(ctx context.Context, addon *api.Addon, preserve bool) (addonExists bool, err error) {
	_, err = a.eksAPI.DeleteAddon(ctx, &eks.DeleteAddonInput{
		AddonName:   &addon.Name,
		ClusterName: &a.clusterConfig.Metadata.Name,
		Preserve:    preserve,
	})

	if err != nil {
		var notFoundErr *ekstypes.ResourceNotFoundException
		if errors.As(err, &notFoundErr) {
			logger.Info("addon %q does not exist", addon.Name)
			return false, nil
		}
		return true, fmt.Errorf("failed to delete addon %q: %v", addon.Name, err)
	}
	return true, nil
}

type Remover struct {
	stackManager StackManager
}

func NewRemover(stackManager StackManager) *Remover {
	return &Remover{
		stackManager: stackManager,
	}
}

func (ar *Remover) DeleteAddonIAMTasks(ctx context.Context, wait bool) (*tasks.TaskTree, error) {
	stacks, err := ar.stackManager.GetIAMAddonsStacks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch addons stacks: %v", err)
	}
	taskTree := &tasks.TaskTree{Parallel: true}
	for _, s := range stacks {
		taskTree.Append(&deleteAddonIAMTask{
			ctx:          ctx,
			info:         fmt.Sprintf("delete addon IAM %q", *s.StackName),
			stack:        s,
			stackManager: ar.stackManager,
			wait:         wait,
		})
	}
	return taskTree, nil
}
