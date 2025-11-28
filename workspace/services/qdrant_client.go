package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/qdrant/go-client/qdrant"
)

// QdrantClient handles communication with Qdrant vector database using the official Go client
type QdrantClient struct {
	client *qdrant.Client
}

// Point represents a vector point in Qdrant
type Point struct {
	ID      string
	Vector  []float32
	Payload map[string]interface{}
}

// SearchResult represents a search result from Qdrant
type SearchResult struct {
	ID      string
	Score   float32
	Payload map[string]interface{}
}

// CollectionInfo represents collection information
type CollectionInfo struct {
	Name    string
	Vectors uint64
	Indexed bool
	Status  string
}

// NewQdrantClient creates a new Qdrant client using the official Go client
func NewQdrantClient(baseURL string) *QdrantClient {
	log.Printf("Creating Qdrant client with URL: %s", baseURL)

	// Parse the URL to extract host and port
	host := baseURL
	port := 6334 // Default gRPC port

	// Remove http:// or https:// prefix if present
	if strings.HasPrefix(baseURL, "http://") {
		host = strings.TrimPrefix(baseURL, "http://")
	} else if strings.HasPrefix(baseURL, "https://") {
		host = strings.TrimPrefix(baseURL, "https://")
	}

	// Remove port number from host if present
	if strings.Contains(host, ":") {
		parts := strings.Split(host, ":")
		host = parts[0]
	}

	log.Printf("Parsed host: %s, port: %d", host, port)

	// Create client configuration
	config := &qdrant.Config{
		Host: host,
		Port: port,
	}

	// Create the client
	client, err := qdrant.NewClient(config)
	if err != nil {
		log.Printf("Failed to create Qdrant client: %v", err)
		return &QdrantClient{client: nil}
	}

	return &QdrantClient{
		client: client,
	}
}

// IsAvailable checks if Qdrant is available
func (q *QdrantClient) IsAvailable() bool {
	if q.client == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try to list collections as a health check
	_, err := q.client.ListCollections(ctx)
	return err == nil
}

// CreateCollection creates a new collection in Qdrant
func (q *QdrantClient) CreateCollection(collectionName string, vectorSize int) error {
	if q.client == nil {
		return fmt.Errorf("Qdrant client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create collection configuration
	config := &qdrant.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     uint64(vectorSize),
			Distance: qdrant.Distance_Cosine,
		}),
	}

	// Create the collection
	err := q.client.CreateCollection(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create collection: %w", err)
	}

	log.Printf("Successfully created collection '%s' with vector size %d", collectionName, vectorSize)
	return nil
}

// CollectionExists checks if a collection exists
func (q *QdrantClient) CollectionExists(collectionName string) (bool, error) {
	if q.client == nil {
		return false, fmt.Errorf("Qdrant client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := q.client.CollectionExists(ctx, collectionName)
	if err != nil {
		return false, fmt.Errorf("failed to check collection: %w", err)
	}

	return exists, nil
}

// UpsertPoints upserts points to a collection
func (q *QdrantClient) UpsertPoints(collectionName string, points []Point) error {
	if q.client == nil {
		return fmt.Errorf("Qdrant client not initialized")
	}

	if len(points) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Convert our Point structs to Qdrant PointStruct
	var qdrantPoints []*qdrant.PointStruct
	for _, point := range points {
		qdrantPoint := &qdrant.PointStruct{
			Id:      qdrant.NewID(point.ID),
			Vectors: qdrant.NewVectors(point.Vector...),
			Payload: qdrant.NewValueMap(point.Payload),
		}

		qdrantPoints = append(qdrantPoints, qdrantPoint)
	}

	// Create upsert request
	wait := true
	upsertRequest := &qdrant.UpsertPoints{
		CollectionName: collectionName,
		Points:         qdrantPoints,
		Wait:           &wait,
	}

	// Execute upsert
	_, err := q.client.Upsert(ctx, upsertRequest)
	if err != nil {
		return fmt.Errorf("failed to upsert points: %w", err)
	}

	log.Printf("Successfully upserted %d points to collection '%s'", len(points), collectionName)
	return nil
}

// SearchPoints searches for similar points in a collection
func (q *QdrantClient) SearchPoints(collectionName string, queryVector []float32, filter map[string]interface{}, limit int) ([]SearchResult, error) {
	log.Printf("DEBUG: QdrantClient.SearchPoints called - collection: %s, vector dims: %d, limit: %d", collectionName, len(queryVector), limit)

	if q.client == nil {
		log.Printf("DEBUG: Qdrant client not initialized")
		return nil, fmt.Errorf("Qdrant client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Convert filter to Qdrant filter format
	var qdrantFilter *qdrant.Filter
	if len(filter) > 0 {
		qdrantFilter = q.convertFilter(filter)
		log.Printf("DEBUG: Using Qdrant filter: %+v", qdrantFilter)
	} else {
		log.Printf("DEBUG: No filter specified")
	}

	// Create query request
	limitUint := uint64(limit)
	queryRequest := &qdrant.QueryPoints{
		CollectionName: collectionName,
		Query:          qdrant.NewQuery(queryVector...),
		Filter:         qdrantFilter,
		Limit:          &limitUint,
		WithPayload:    qdrant.NewWithPayload(true),
		WithVectors:    qdrant.NewWithVectors(false),
	}

	log.Printf("DEBUG: Executing Qdrant query...")
	// Execute query
	queryResult, err := q.client.Query(ctx, queryRequest)
	if err != nil {
		log.Printf("DEBUG: Qdrant query failed: %v", err)
		return nil, fmt.Errorf("failed to query points: %w", err)
	}

	log.Printf("DEBUG: Qdrant query returned %d points", len(queryResult))

	// Convert results to our format
	var results []SearchResult
	log.Printf("DEBUG: Processing %d query results", len(queryResult))

	for i, point := range queryResult {
		log.Printf("DEBUG: Processing point %d - Score: %.4f", i+1, point.Score)

		// Convert payload back to our format
		payload := make(map[string]interface{})
		for key, value := range point.Payload {
			payload[key] = q.convertValueToInterface(value)
		}

		result := SearchResult{
			ID:      q.convertPointIDToString(point.Id),
			Score:   point.Score,
			Payload: payload,
		}

		results = append(results, result)
		log.Printf("DEBUG: Added result with file_path: %s", payload["file_path"])
	}

	log.Printf("DEBUG: QdrantClient.SearchPoints returning %d results", len(results))
	return results, nil
}

// DeletePoints deletes points from a collection
func (q *QdrantClient) DeletePoints(collectionName string, pointIDs []string) error {
	if q.client == nil {
		return fmt.Errorf("Qdrant client not initialized")
	}

	if len(pointIDs) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Convert string IDs to Qdrant IDs
	var qdrantIDs []*qdrant.PointId
	for _, id := range pointIDs {
		qdrantIDs = append(qdrantIDs, qdrant.NewID(id))
	}

	// Create delete request
	wait := true
	deleteRequest := &qdrant.DeletePoints{
		CollectionName: collectionName,
		Points: &qdrant.PointsSelector{
			PointsSelectorOneOf: &qdrant.PointsSelector_Points{
				Points: &qdrant.PointsIdsList{Ids: qdrantIDs},
			},
		},
		Wait: &wait,
	}

	// Execute delete
	_, err := q.client.Delete(ctx, deleteRequest)
	if err != nil {
		return fmt.Errorf("failed to delete points: %w", err)
	}

	log.Printf("Successfully deleted %d points from collection '%s'", len(pointIDs), collectionName)
	return nil
}

// GetCollectionInfo gets information about a collection
func (q *QdrantClient) GetCollectionInfo(collectionName string) (*CollectionInfo, error) {
	if q.client == nil {
		return nil, fmt.Errorf("Qdrant client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collectionInfo, err := q.client.GetCollectionInfo(ctx, collectionName)
	if err != nil {
		return nil, fmt.Errorf("failed to get collection info: %w", err)
	}

	info := &CollectionInfo{
		Name:    collectionName,
		Vectors: collectionInfo.GetPointsCount(),
		Indexed: collectionInfo.GetIndexedVectorsCount() > 0,
		Status:  collectionInfo.GetStatus().String(),
	}

	return info, nil
}

// DeleteCollection deletes a collection
func (q *QdrantClient) DeleteCollection(collectionName string) error {
	if q.client == nil {
		return fmt.Errorf("Qdrant client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := q.client.DeleteCollection(ctx, collectionName)
	if err != nil {
		return fmt.Errorf("failed to delete collection: %w", err)
	}

	log.Printf("Successfully deleted collection '%s'", collectionName)
	return nil
}

// convertFilter converts our filter format to Qdrant filter format
func (q *QdrantClient) convertFilter(filter map[string]interface{}) *qdrant.Filter {
	// This is a simplified filter conversion
	// In practice, you'd want to handle more complex filter structures

	var conditions []*qdrant.Condition

	for key, value := range filter {
		if must, ok := value.([]map[string]interface{}); ok {
			// Handle "must" conditions
			for _, condition := range must {
				if match, exists := condition["match"]; exists {
					if matchMap, ok := match.(map[string]interface{}); ok {
						if val, exists := matchMap["value"]; exists {
							if strVal, ok := val.(string); ok {
								condition := qdrant.NewMatchKeyword(key, strVal)
								conditions = append(conditions, condition)
							}
						}
					}
				}
			}
		}
	}

	if len(conditions) == 0 {
		return nil
	}

	return &qdrant.Filter{
		Must: conditions,
	}
}

// convertValueToInterface converts Qdrant Value to Go interface{}
func (q *QdrantClient) convertValueToInterface(value *qdrant.Value) interface{} {
	if value == nil {
		return nil
	}

	switch v := value.GetKind().(type) {
	case *qdrant.Value_StringValue:
		return v.StringValue
	case *qdrant.Value_IntegerValue:
		return v.IntegerValue
	case *qdrant.Value_DoubleValue:
		return v.DoubleValue
	case *qdrant.Value_BoolValue:
		return v.BoolValue
	case *qdrant.Value_NullValue:
		return nil
	default:
		return value.String() // Fallback to string representation
	}
}

// convertPointIDToString converts Qdrant PointId to string
func (q *QdrantClient) convertPointIDToString(pointID *qdrant.PointId) string {
	if pointID == nil {
		return ""
	}

	switch id := pointID.GetPointIdOptions().(type) {
	case *qdrant.PointId_Uuid:
		return id.Uuid
	case *qdrant.PointId_Num:
		return fmt.Sprintf("%d", id.Num)
	default:
		return pointID.String() // Fallback to string representation
	}
}

// Close closes the Qdrant client connection
func (q *QdrantClient) Close() error {
	if q.client != nil {
		return q.client.Close()
	}
	return nil
}
