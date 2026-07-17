package ocm

import (
	"context"
	"fmt"

	sdk "github.com/openshift-online/ocm-sdk-go"
	cmv1 "github.com/openshift-online/ocm-sdk-go/clustersmgmt/v1"
)

type sdkClient struct {
	conn *sdk.Connection
}

func NewSDKClient(token string, clientID string, urls ...string) (OCMClient, error) {
	builder := sdk.NewConnectionBuilder().
		Tokens(token)
	if clientID != "" {
		builder = builder.Client(clientID, "")
	}
	if len(urls) > 0 && urls[0] != "" {
		builder = builder.URL(urls[0])
	}
	conn, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("building OCM connection: %w", err)
	}
	return &sdkClient{conn: conn}, nil
}

func (c *sdkClient) ListClusters(ctx context.Context, search string, page, size int) ([]ClusterMetadata, int, error) {
	req := c.conn.ClustersMgmt().V1().Clusters().List().
		Search(search).
		Page(page).
		Size(size)

	resp, err := req.SendContext(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("OCM API request failed: %w", err)
	}

	var clusters []ClusterMetadata
	resp.Items().Each(func(cluster *cmv1.Cluster) bool {
		m := ClusterMetadata{
			ID:               cluster.ID(),
			Name:             cluster.Name(),
			ExternalID:       cluster.ExternalID(),
			State:            string(cluster.State()),
			OpenshiftVersion: cluster.OpenshiftVersion(),
			MultiAZ:          cluster.MultiAZ(),
			Managed:          cluster.Managed(),
			CreationTimestamp: cluster.CreationTimestamp(),
		}
		if cluster.Product() != nil {
			m.Product = cluster.Product().ID()
		}
		if cluster.CloudProvider() != nil {
			m.CloudProvider = cluster.CloudProvider().ID()
		}
		if cluster.Region() != nil {
			m.Region = cluster.Region().ID()
		}
		if hs, ok := cluster.GetHealthState(); ok {
			m.HealthState = string(hs)
		}
		clusters = append(clusters, m)
		return true
	})

	return clusters, resp.Total(), nil
}
