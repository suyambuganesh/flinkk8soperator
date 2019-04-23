package flink

import (
	"context"
	"testing"

	"time"

	"github.com/lyft/flinkk8soperator/pkg/apis/app/v1alpha1"
	"github.com/lyft/flinkk8soperator/pkg/controller/common"
	"github.com/lyft/flinkk8soperator/pkg/controller/flink/client"
	clientMock "github.com/lyft/flinkk8soperator/pkg/controller/flink/client/mock"
	"github.com/lyft/flinkk8soperator/pkg/controller/flink/mock"
	k8mock "github.com/lyft/flinkk8soperator/pkg/controller/k8/mock"
	mockScope "github.com/lyft/flytestdlib/promutils"
	"github.com/lyft/flytestdlib/promutils/labeled"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const testImage = "123.xyz.com/xx:11ae1218924428faabd9b64423fa0c332efba6b2"

// Note: if you find yourself changing this to fix a test, that should be treated as a breaking API change
const testAppHash = "8dd2d4a3"
const testAppName = "app-name"
const testNamespace = "ns"
const testJobID = "j1"
const testFlinkVersion = "1.7"

func getTestFlinkController() Controller {
	testScope := mockScope.NewTestScope()
	labeled.SetMetricKeys(common.GetValidLabelNames()...)
	return Controller{
		jobManager:  &mock.JobManagerController{},
		taskManager: &mock.TaskManagerController{},
		k8Cluster:   &k8mock.K8Cluster{},
		flinkClient: &clientMock.JobManagerClient{},
		metrics:     newControllerMetrics(testScope),
	}
}

func getFlinkTestApp() v1alpha1.FlinkApplication {
	app := v1alpha1.FlinkApplication{}
	app.Spec.Parallelism = 8
	app.Name = testAppName
	app.Namespace = testNamespace
	app.Status.JobID = testJobID
	app.Spec.Image = testImage
	app.Spec.FlinkVersion = testFlinkVersion

	return app
}

func getDeployment(app *v1alpha1.FlinkApplication) v1.Deployment {
	d := v1.Deployment{}
	d.Name = app.Name + "-" + testAppHash + "-tm"
	d.Spec = v1.DeploymentSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Image: app.Spec.Image,
					},
				},
			},
		},
	}
	d.Labels = map[string]string{
		"flink-deployment-type": "taskmanager",
		"app":                   testAppName,
		"flink-app-hash":        testAppHash,
	}

	return d
}

func TestFlinkIsClusterReady(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	labelMapVal := map[string]string{
		"flink-app-hash": testAppHash,
	}
	mockK8Cluster := flinkControllerForTest.k8Cluster.(*k8mock.K8Cluster)
	mockK8Cluster.GetPodsWithLabelFunc = func(ctx context.Context, namespace string, labelMap map[string]string) (*corev1.PodList, error) {
		assert.Equal(t, testNamespace, namespace)
		assert.Equal(t, labelMapVal, labelMap)

		return &corev1.PodList{
			Items: []corev1.Pod{
				{
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
			},
		}, nil
	}

	flinkApp := getFlinkTestApp()
	result, err := flinkControllerForTest.IsClusterReady(
		context.Background(), &flinkApp,
	)
	assert.True(t, result)
	assert.Nil(t, err)
}

func TestFlinkApplicationChangedReplicas(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	labelMapVal := map[string]string{
		"app": testAppName,
	}

	flinkApp := getFlinkTestApp()
	taskSlots := int32(16)
	flinkApp.Spec.TaskManagerConfig.TaskSlots = &taskSlots
	flinkApp.Spec.Parallelism = 8

	mockK8Cluster := flinkControllerForTest.k8Cluster.(*k8mock.K8Cluster)
	mockK8Cluster.GetDeploymentsWithLabelFunc = func(ctx context.Context, namespace string, labelMap map[string]string) (*v1.DeploymentList, error) {
		assert.Equal(t, testNamespace, namespace)
		assert.Equal(t, labelMapVal, labelMap)

		newApp := flinkApp.DeepCopy()
		newApp.Spec.Parallelism = 10
		d := *FetchTaskMangerDeploymentCreateObj(newApp)

		return &v1.DeploymentList{
			Items: []v1.Deployment{d},
		}, nil
	}

	result, err := flinkControllerForTest.HasApplicationChanged(
		context.Background(), &flinkApp,
	)
	assert.True(t, result)
	assert.Nil(t, err)
}

func TestFlinkApplicationNotChanged(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	labelMapVal := map[string]string{
		"app": testAppName,
	}
	flinkApp := getFlinkTestApp()
	mockK8Cluster := flinkControllerForTest.k8Cluster.(*k8mock.K8Cluster)
	mockK8Cluster.GetDeploymentsWithLabelFunc = func(ctx context.Context, namespace string, labelMap map[string]string) (*v1.DeploymentList, error) {
		assert.Equal(t, testNamespace, namespace)
		assert.Equal(t, labelMapVal, labelMap)
		return &v1.DeploymentList{
			Items: []v1.Deployment{*FetchTaskMangerDeploymentCreateObj(&flinkApp)},
		}, nil
	}
	result, err := flinkControllerForTest.HasApplicationChanged(
		context.Background(), &flinkApp,
	)
	assert.False(t, result)
	assert.Nil(t, err)
}

func TestFlinkApplicationChanged(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	labelMapVal := map[string]string{
		"app": testAppName,
	}
	mockK8Cluster := flinkControllerForTest.k8Cluster.(*k8mock.K8Cluster)
	mockK8Cluster.GetDeploymentsWithLabelFunc = func(ctx context.Context, namespace string, labelMap map[string]string) (*v1.DeploymentList, error) {
		assert.Equal(t, testNamespace, namespace)
		assert.Equal(t, labelMapVal, labelMap)
		return &v1.DeploymentList{}, nil
	}
	flinkApp := getFlinkTestApp()
	result, err := flinkControllerForTest.HasApplicationChanged(
		context.Background(), &flinkApp,
	)
	assert.True(t, result)
	assert.Nil(t, err)
}

func TestFlinkApplicationChangedParallelism(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()

	mockK8Cluster := flinkControllerForTest.k8Cluster.(*k8mock.K8Cluster)
	mockK8Cluster.GetDeploymentsWithLabelFunc = func(ctx context.Context, namespace string, labelMap map[string]string) (*v1.DeploymentList, error) {
		assert.Equal(t, testNamespace, namespace)
		if val, ok := labelMap["flink-app-hash"]; ok {
			assert.Equal(t, testAppHash, val)
		}
		if val, ok := labelMap["App"]; ok {
			assert.Equal(t, testAppName, val)
		}
		deployment := getDeployment(&flinkApp)
		deployment.Name = testAppName + "-" + testAppHash + "-tm"
		return &v1.DeploymentList{
			Items: []v1.Deployment{
				deployment,
			},
		}, nil
	}

	flinkApp.Spec.Parallelism = 3
	result, err := flinkControllerForTest.HasApplicationChanged(
		context.Background(), &flinkApp,
	)
	assert.True(t, result)
	assert.Nil(t, err)
}

func TestFlinkApplicationNeedsUpdate(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	numberOfTaskManagers := int32(2)
	mockK8Cluster := flinkControllerForTest.k8Cluster.(*k8mock.K8Cluster)
	mockK8Cluster.GetDeploymentsWithLabelFunc = func(ctx context.Context, namespace string, labelMap map[string]string) (*v1.DeploymentList, error) {
		assert.Equal(t, testNamespace, namespace)
		if val, ok := labelMap["flink-app-hash"]; ok {
			assert.Equal(t, testAppHash, val)
		}
		if val, ok := labelMap["App"]; ok {
			assert.Equal(t, testAppName, val)
		}
		deployment := v1.Deployment{
			Spec: v1.DeploymentSpec{
				Replicas: &numberOfTaskManagers,
			},
		}
		deployment.Name = testAppName + "-" + testAppHash + "-tm"
		return &v1.DeploymentList{
			Items: []v1.Deployment{
				deployment,
			},
		}, nil
	}
	flinkApp := getFlinkTestApp()
	taskSlots := int32(2)
	flinkApp.Spec.TaskManagerConfig.TaskSlots = &taskSlots
	flinkApp.Spec.Parallelism = taskSlots*numberOfTaskManagers + 1
	result, err := flinkControllerForTest.HasApplicationChanged(
		context.Background(), &flinkApp,
	)
	assert.True(t, result)
	assert.Nil(t, err)
}

func TestFlinkIsServiceReady(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()

	mockJmClient := flinkControllerForTest.flinkClient.(*clientMock.JobManagerClient)
	mockJmClient.GetClusterOverviewFunc = func(ctx context.Context, url string) (*client.ClusterOverviewResponse, error) {
		assert.Equal(t, url, "http://app-name-jm.ns:8081")
		return &client.ClusterOverviewResponse{
			TaskManagerCount: 3,
		}, nil
	}
	isReady, err := flinkControllerForTest.IsServiceReady(context.Background(), &flinkApp)
	assert.Nil(t, err)
	assert.True(t, isReady)
}

func TestFlinkIsServiceReadyErr(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()

	mockJmClient := flinkControllerForTest.flinkClient.(*clientMock.JobManagerClient)
	mockJmClient.GetClusterOverviewFunc = func(ctx context.Context, url string) (*client.ClusterOverviewResponse, error) {
		assert.Equal(t, url, "http://app-name-jm.ns:8081")
		return nil, errors.New("Get cluster failed")
	}
	isReady, err := flinkControllerForTest.IsServiceReady(context.Background(), &flinkApp)
	assert.EqualError(t, err, "Get cluster failed")
	assert.False(t, isReady)
}

func TestFlinkGetSavepointStatus(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()
	flinkApp.Spec.SavepointInfo.TriggerID = "t1"

	mockJmClient := flinkControllerForTest.flinkClient.(*clientMock.JobManagerClient)
	mockJmClient.CheckSavepointStatusFunc = func(ctx context.Context, url string, jobID, triggerID string) (*client.SavepointResponse, error) {
		assert.Equal(t, url, "http://app-name-jm.ns:8081")
		assert.Equal(t, jobID, testJobID)
		assert.Equal(t, triggerID, "t1")
		return &client.SavepointResponse{
			SavepointStatus: client.SavepointStatusResponse{
				Status: client.SavePointInProgress,
			},
		}, nil
	}
	status, err := flinkControllerForTest.GetSavepointStatus(context.Background(), &flinkApp)
	assert.Nil(t, err)
	assert.NotNil(t, status)

	assert.Equal(t, client.SavePointInProgress, status.SavepointStatus.Status)
}

func TestFlinkGetSavepointStatusErr(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()

	mockJmClient := flinkControllerForTest.flinkClient.(*clientMock.JobManagerClient)
	mockJmClient.CheckSavepointStatusFunc = func(ctx context.Context, url string, jobID, triggerID string) (*client.SavepointResponse, error) {
		assert.Equal(t, url, "http://app-name-jm.ns:8081")
		assert.Equal(t, jobID, testJobID)
		return nil, errors.New("Savepoint error")
	}
	status, err := flinkControllerForTest.GetSavepointStatus(context.Background(), &flinkApp)
	assert.Nil(t, status)
	assert.NotNil(t, err)

	assert.EqualError(t, err, "Savepoint error")
}

func TestGetActiveJob(t *testing.T) {
	job := client.FlinkJob{
		Status: client.FlinkJobRunning,
		JobID:  "j1",
	}
	jobs := []client.FlinkJob{
		job,
	}
	activeJob := GetActiveFlinkJob(jobs)
	assert.NotNil(t, activeJob)
	assert.Equal(t, *activeJob, job)
}

func TestGetActiveJobNil(t *testing.T) {
	job := client.FlinkJob{
		Status: client.FlinkJobCancelling,
		JobID:  "j1",
	}
	jobs := []client.FlinkJob{
		job,
	}
	activeJob := GetActiveFlinkJob(jobs)
	assert.Nil(t, activeJob)
}

func TestGetActiveJobEmpty(t *testing.T) {
	jobs := []client.FlinkJob{}
	activeJob := GetActiveFlinkJob(jobs)
	assert.Nil(t, activeJob)
}

func TestDeleteOldCluster(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()
	labelMapVal := map[string]string{
		"app": testAppName,
	}
	d1 := *FetchTaskMangerDeploymentCreateObj(&flinkApp)
	d2 := *FetchTaskMangerDeploymentCreateObj(&flinkApp)
	d2.Labels = map[string]string{
		"flink-app-hash": testAppHash + "3",
	}
	mockK8Cluster := flinkControllerForTest.k8Cluster.(*k8mock.K8Cluster)
	mockK8Cluster.GetDeploymentsWithLabelFunc = func(ctx context.Context, namespace string, labelMap map[string]string) (*v1.DeploymentList, error) {
		assert.Equal(t, testNamespace, namespace)
		assert.Equal(t, labelMapVal, labelMap)

		return &v1.DeploymentList{
			Items: []v1.Deployment{
				d1, d2,
			},
		}, nil
	}
	mockK8Cluster.DeleteDeploymentsFunc = func(ctx context.Context, deploymentList v1.DeploymentList) error {
		assert.Equal(t, v1.DeploymentList{Items: []v1.Deployment{d2}}, deploymentList)
		return nil
	}
	isDeleted, err := flinkControllerForTest.DeleteOldCluster(context.Background(), &flinkApp)
	assert.Nil(t, err)
	assert.True(t, isDeleted)
}

func TestDeleteOldClusterNoOldDeployment(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()
	labelMapVal := map[string]string{
		"app": testAppName,
	}
	mockK8Cluster := flinkControllerForTest.k8Cluster.(*k8mock.K8Cluster)
	mockK8Cluster.GetDeploymentsWithLabelFunc = func(ctx context.Context, namespace string, labelMap map[string]string) (*v1.DeploymentList, error) {
		assert.Equal(t, testNamespace, namespace)
		assert.Equal(t, labelMapVal, labelMap)
		d1 := *FetchTaskMangerDeploymentCreateObj(&flinkApp)

		return &v1.DeploymentList{Items: []v1.Deployment{
			d1,
		}}, nil
	}
	mockK8Cluster.DeleteDeploymentsFunc = func(ctx context.Context, deploymentList v1.DeploymentList) error {
		assert.False(t, true)
		return nil
	}
	isDeleted, err := flinkControllerForTest.DeleteOldCluster(context.Background(), &flinkApp)
	assert.Nil(t, err)
	assert.True(t, isDeleted)
}

func TestDeleteOldClusterNoDeployment(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()
	labelMapVal := map[string]string{
		"app": testAppName,
	}
	mockK8Cluster := flinkControllerForTest.k8Cluster.(*k8mock.K8Cluster)
	mockK8Cluster.GetDeploymentsWithLabelFunc = func(ctx context.Context, namespace string, labelMap map[string]string) (*v1.DeploymentList, error) {
		assert.Equal(t, testNamespace, namespace)
		assert.Equal(t, labelMapVal, labelMap)
		return &v1.DeploymentList{}, nil
	}
	mockK8Cluster.DeleteDeploymentsFunc = func(ctx context.Context, deploymentList v1.DeploymentList) error {
		assert.False(t, true)
		return nil
	}
	isDeleted, err := flinkControllerForTest.DeleteOldCluster(context.Background(), &flinkApp)
	assert.Nil(t, err)
	assert.True(t, isDeleted)
}

func TestDeleteOldClusterErr(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()
	labelMapVal := map[string]string{
		"app": testAppName,
	}
	d1 := v1.Deployment{}
	d1.Labels = labelMapVal

	mockK8Cluster := flinkControllerForTest.k8Cluster.(*k8mock.K8Cluster)
	mockK8Cluster.GetDeploymentsWithLabelFunc = func(ctx context.Context, namespace string, labelMap map[string]string) (*v1.DeploymentList, error) {
		assert.Equal(t, testNamespace, namespace)
		assert.Equal(t, labelMapVal, labelMap)
		return &v1.DeploymentList{
			Items: []v1.Deployment{
				d1,
			},
		}, nil
	}
	mockK8Cluster.DeleteDeploymentsFunc = func(ctx context.Context, deploymentList v1.DeploymentList) error {
		assert.Equal(t, v1.DeploymentList{Items: []v1.Deployment{d1}}, deploymentList)
		return errors.New("Delete error")
	}
	isDeleted, err := flinkControllerForTest.DeleteOldCluster(context.Background(), &flinkApp)
	assert.EqualError(t, err, "Delete error")
	assert.False(t, isDeleted)
}

func TestCreateCluster(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()
	mockJobManager := flinkControllerForTest.jobManager.(*mock.JobManagerController)
	mockTaskManager := flinkControllerForTest.taskManager.(*mock.TaskManagerController)

	mockJobManager.CreateIfNotExistFunc = func(ctx context.Context, application *v1alpha1.FlinkApplication) error {
		return nil
	}
	mockTaskManager.CreateIfNotExistFunc = func(ctx context.Context, application *v1alpha1.FlinkApplication) error {
		return nil
	}
	err := flinkControllerForTest.CreateCluster(context.Background(), &flinkApp)
	assert.Nil(t, err)
}

func TestCreateClusterJmErr(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()
	mockJobManager := flinkControllerForTest.jobManager.(*mock.JobManagerController)
	mockTaskManager := flinkControllerForTest.taskManager.(*mock.TaskManagerController)

	mockJobManager.CreateIfNotExistFunc = func(ctx context.Context, application *v1alpha1.FlinkApplication) error {
		return errors.New("jm failed")
	}
	mockTaskManager.CreateIfNotExistFunc = func(ctx context.Context, application *v1alpha1.FlinkApplication) error {
		assert.False(t, true)
		return nil
	}
	err := flinkControllerForTest.CreateCluster(context.Background(), &flinkApp)
	assert.EqualError(t, err, "jm failed")
}

func TestCreateClusterTmErr(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()
	mockJobManager := flinkControllerForTest.jobManager.(*mock.JobManagerController)
	mockTaskManager := flinkControllerForTest.taskManager.(*mock.TaskManagerController)

	mockJobManager.CreateIfNotExistFunc = func(ctx context.Context, application *v1alpha1.FlinkApplication) error {
		return nil
	}
	mockTaskManager.CreateIfNotExistFunc = func(ctx context.Context, application *v1alpha1.FlinkApplication) error {
		return errors.New("tm failed")
	}
	err := flinkControllerForTest.CreateCluster(context.Background(), &flinkApp)
	assert.EqualError(t, err, "tm failed")
}

func TestStartFlinkJob(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()
	flinkApp.Spec.Parallelism = 4
	flinkApp.Spec.ProgramArgs = "args"
	flinkApp.Spec.EntryClass = "class"
	flinkApp.Spec.JarName = "jar-name"
	flinkApp.Spec.SavepointInfo.SavepointLocation = "location//"
	flinkApp.Spec.FlinkVersion = "1.7"

	mockJmClient := flinkControllerForTest.flinkClient.(*clientMock.JobManagerClient)
	mockJmClient.SubmitJobFunc = func(ctx context.Context, url string, jarID string, submitJobRequest client.SubmitJobRequest) (*client.SubmitJobResponse, error) {
		assert.Equal(t, url, "http://app-name-jm.ns:8081")
		assert.Equal(t, jarID, "jar-name")
		assert.Equal(t, submitJobRequest.Parallelism, int32(4))
		assert.Equal(t, submitJobRequest.ProgramArgs, "args")
		assert.Equal(t, submitJobRequest.EntryClass, "class")
		assert.Equal(t, submitJobRequest.SavepointPath, "location//")

		return &client.SubmitJobResponse{
			JobID: testJobID,
		}, nil
	}
	jobID, err := flinkControllerForTest.StartFlinkJob(context.Background(), &flinkApp)
	assert.Nil(t, err)
	assert.Equal(t, jobID, testJobID)
}

func TestStartFlinkJobEmptyJobID(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()

	mockJmClient := flinkControllerForTest.flinkClient.(*clientMock.JobManagerClient)
	mockJmClient.SubmitJobFunc = func(ctx context.Context, url string, jarID string, submitJobRequest client.SubmitJobRequest) (*client.SubmitJobResponse, error) {

		return &client.SubmitJobResponse{}, nil
	}
	jobID, err := flinkControllerForTest.StartFlinkJob(context.Background(), &flinkApp)
	assert.EqualError(t, err, "unable to submit job: invalid job id")
	assert.Empty(t, jobID)
}

func TestStartFlinkJobErr(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()

	mockJmClient := flinkControllerForTest.flinkClient.(*clientMock.JobManagerClient)
	mockJmClient.SubmitJobFunc = func(ctx context.Context, url string, jarID string, submitJobRequest client.SubmitJobRequest) (*client.SubmitJobResponse, error) {
		return nil, errors.New("submit error")
	}
	jobID, err := flinkControllerForTest.StartFlinkJob(context.Background(), &flinkApp)
	assert.EqualError(t, err, "submit error")
	assert.Empty(t, jobID)
}

func TestCancelWithSavepoint(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()

	mockJmClient := flinkControllerForTest.flinkClient.(*clientMock.JobManagerClient)
	mockJmClient.CancelJobWithSavepointFunc = func(ctx context.Context, url string, jobID string) (string, error) {
		assert.Equal(t, url, "http://app-name-jm.ns:8081")
		assert.Equal(t, jobID, testJobID)
		return "t1", nil
	}
	triggerID, err := flinkControllerForTest.CancelWithSavepoint(context.Background(), &flinkApp)
	assert.Nil(t, err)
	assert.Equal(t, triggerID, "t1")
}

func TestCancelWithSavepointErr(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()

	mockJmClient := flinkControllerForTest.flinkClient.(*clientMock.JobManagerClient)
	mockJmClient.CancelJobWithSavepointFunc = func(ctx context.Context, url string, jobID string) (string, error) {
		return "", errors.New("cancel error")
	}
	triggerID, err := flinkControllerForTest.CancelWithSavepoint(context.Background(), &flinkApp)
	assert.EqualError(t, err, "cancel error")
	assert.Empty(t, triggerID)
}

func TestGetJobsForApplication(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()

	mockJmClient := flinkControllerForTest.flinkClient.(*clientMock.JobManagerClient)
	mockJmClient.GetJobsFunc = func(ctx context.Context, url string) (*client.GetJobsResponse, error) {
		assert.Equal(t, url, "http://app-name-jm.ns:8081")
		return &client.GetJobsResponse{
			Jobs: []client.FlinkJob{
				{
					JobID: testJobID,
				},
			},
		}, nil
	}
	jobs, err := flinkControllerForTest.GetJobsForApplication(context.Background(), &flinkApp)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(jobs))
	assert.Equal(t, jobs[0].JobID, testJobID)
}

func TestGetJobsForApplicationErr(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()

	mockJmClient := flinkControllerForTest.flinkClient.(*clientMock.JobManagerClient)
	mockJmClient.GetJobsFunc = func(ctx context.Context, url string) (*client.GetJobsResponse, error) {
		return nil, errors.New("get jobs error")
	}
	jobs, err := flinkControllerForTest.GetJobsForApplication(context.Background(), &flinkApp)
	assert.EqualError(t, err, "get jobs error")
	assert.Nil(t, jobs)
}

func TestFindExternalizedCheckpoint(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()
	flinkApp.Status.JobID = "jobid"

	mockJmClient := flinkControllerForTest.flinkClient.(*clientMock.JobManagerClient)
	mockJmClient.GetLatestCheckpointFunc = func(ctx context.Context, url string, jobId string) (*client.CheckpointStatistics, error) {
		assert.Equal(t, url, "http://app-name-jm.ns:8081")
		assert.Equal(t, "jobid", jobId)
		return &client.CheckpointStatistics{
			TriggerTimestamp: time.Now().Unix(),
			ExternalPath:     "/tmp/checkpoint",
		}, nil
	}

	checkpoint, err := flinkControllerForTest.FindExternalizedCheckpoint(context.Background(), &flinkApp)
	assert.Nil(t, err)
	assert.Equal(t, "/tmp/checkpoint", checkpoint)
}

func TestGetAndUpdateClusterStatus(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()

	mockJmClient := flinkControllerForTest.flinkClient.(*clientMock.JobManagerClient)
	mockJmClient.GetClusterOverviewFunc = func(ctx context.Context, url string) (*client.ClusterOverviewResponse, error) {
		assert.Equal(t, url, "http://app-name-jm.ns:8081")
		return &client.ClusterOverviewResponse{
			NumberOfTaskSlots: 1,
			SlotsAvailable:    0,
			TaskManagerCount:  1,
		}, nil
	}

	mockJmClient.GetTaskManagersFunc = func(ctx context.Context, url string) (*client.TaskManagersResponse, error) {
		assert.Equal(t, url, "http://app-name-jm.ns:8081")
		return &client.TaskManagersResponse{
			TaskManagers: []client.TaskManagerStats{
				{
					TimeSinceLastHeartbeat: time.Now().UnixNano() / int64(time.Millisecond),
					SlotsNumber:            3,
					FreeSlots:              0,
				},
			},
		}, nil
	}

	err := flinkControllerForTest.GetAndUpdateClusterStatus(context.Background(), &flinkApp)
	assert.Nil(t, err)
	assert.Equal(t, int32(1), flinkApp.Status.ClusterStatus.NumberOfTaskSlots)
	assert.Equal(t, int32(0), flinkApp.Status.ClusterStatus.AvailableTaskSlots)
	assert.Equal(t, int32(1), flinkApp.Status.ClusterStatus.HealthyTaskManagers)
	assert.Equal(t, v1alpha1.Green, flinkApp.Status.ClusterStatus.Health)

}

func TestHealthyTaskmanagers(t *testing.T) {
	flinkControllerForTest := getTestFlinkController()
	flinkApp := getFlinkTestApp()

	mockJmClient := flinkControllerForTest.flinkClient.(*clientMock.JobManagerClient)

	mockJmClient.GetClusterOverviewFunc = func(ctx context.Context, url string) (*client.ClusterOverviewResponse, error) {
		assert.Equal(t, url, "http://app-name-jm.ns:8081")
		return &client.ClusterOverviewResponse{
			NumberOfTaskSlots: 1,
			SlotsAvailable:    0,
			TaskManagerCount:  1,
		}, nil
	}

	mockJmClient.GetTaskManagersFunc = func(ctx context.Context, url string) (*client.TaskManagersResponse, error) {
		assert.Equal(t, url, "http://app-name-jm.ns:8081")
		return &client.TaskManagersResponse{
			TaskManagers: []client.TaskManagerStats{
				{
					// 1 day old
					TimeSinceLastHeartbeat: time.Now().AddDate(0, 0, -1).UnixNano() / int64(time.Millisecond),
					SlotsNumber:            3,
					FreeSlots:              0,
				},
			},
		}, nil
	}

	err := flinkControllerForTest.GetAndUpdateClusterStatus(context.Background(), &flinkApp)
	assert.Nil(t, err)
	assert.Equal(t, int32(1), flinkApp.Status.ClusterStatus.NumberOfTaskSlots)
	assert.Equal(t, int32(0), flinkApp.Status.ClusterStatus.AvailableTaskSlots)
	assert.Equal(t, int32(0), flinkApp.Status.ClusterStatus.HealthyTaskManagers)
	assert.Equal(t, v1alpha1.Yellow, flinkApp.Status.ClusterStatus.Health)

}
