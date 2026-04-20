package clients

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "notes-bot/proto/search"
)

// SearchClient wraps the gRPC search service
type SearchClient struct {
	conn   *grpc.ClientConn
	client pb.SearchServiceClient
}

// SearchNote represents a single search result
type SearchNote struct {
	Date    string
	Content string
	Score   float32
}

// NewSearchClient creates a new search client
func NewSearchClient(addr string) (*SearchClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to search service: %w", err)
	}

	return &SearchClient{
		conn:   conn,
		client: pb.NewSearchServiceClient(conn),
	}, nil
}

// Close closes the connection
func (c *SearchClient) Close() error {
	return c.conn.Close()
}

// IndexNotes indexes all notes from the specified path
func (c *SearchClient) IndexNotes(ctx context.Context, notesPath string) (int32, error) {
	resp, err := c.client.IndexNotes(ctx, &pb.IndexNotesRequest{
		NotesPath: notesPath,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to index notes: %w", err)
	}
	if !resp.Success {
		return 0, fmt.Errorf("indexing failed: %s", resp.Error)
	}
	return resp.IndexedCount, nil
}

// SearchNotes searches notes by query
func (c *SearchClient) SearchNotes(ctx context.Context, query string, limit int32) ([]SearchNote, error) {
	resp, err := c.client.SearchNotes(ctx, &pb.SearchNotesRequest{
		Query: query,
		Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search notes: %w", err)
	}

	results := make([]SearchNote, len(resp.Results))
	for i, r := range resp.Results {
		results[i] = SearchNote{
			Date:    r.Date,
			Content: r.Content,
			Score:   r.Score,
		}
	}
	return results, nil
}

// GetIndexStatus returns the current index status
func (c *SearchClient) GetIndexStatus(ctx context.Context) (int32, int64, error) {
	resp, err := c.client.GetIndexStatus(ctx, &pb.GetIndexStatusRequest{})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get index status: %w", err)
	}
	return resp.IndexedCount, resp.LastIndexedAt, nil
}
