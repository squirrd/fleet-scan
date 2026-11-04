package collector

import (
	"context"
	"time"
)

type fakeCCOData struct {
	credentialsMode      string
	serviceAccountIssuer string
	logs                 string
	logsErr              error
}

type fakeCCOLister struct {
	data fakeCCOData
}

func (f *fakeCCOLister) GetCredentialsMode(ctx context.Context) (string, error) {
	return f.data.credentialsMode, nil
}

func (f *fakeCCOLister) GetServiceAccountIssuer(ctx context.Context) (string, error) {
	return f.data.serviceAccountIssuer, nil
}

func (f *fakeCCOLister) GetCCOLogs(ctx context.Context, window time.Duration) (string, error) {
	if f.data.logsErr != nil {
		return "", f.data.logsErr
	}
	return f.data.logs, nil
}

func fakeCCOClientBuilder(data fakeCCOData) ccoClientBuilderFunc {
	return func(kubeconfigPath string) (ccoLister, error) {
		return &fakeCCOLister{data: data}, nil
	}
}
