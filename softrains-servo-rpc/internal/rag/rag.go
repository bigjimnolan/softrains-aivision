package rag

import (
	"fmt"
	"os"

	"github.com/sirupsen/logrus"

	"github.com/blevesearch/bleve"
)

type Engine struct {
	index  bleve.Index
	logger *logrus.Logger
}

type Document struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

func NewEngine(indexPath string, logger *logrus.Logger) (*Engine, error) {
	var idx bleve.Index
	var err error

	if _, err = os.Stat(indexPath); os.IsNotExist(err) {
		mapping := bleve.NewIndexMapping()
		idx, err = bleve.New(indexPath, mapping)
		if err != nil {
			return nil, fmt.Errorf("failed to create index: %w", err)
		}
	} else {
		idx, err = bleve.Open(indexPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open index: %w", err)
		}
	}

	return &Engine{index: idx, logger: logger}, nil
}

func (e *Engine) Close() error {
	return e.index.Close()
}

func (e *Engine) IndexDocument(doc Document) error {
	e.logger.Debugf("Indexing document ID: %v", doc)
	return e.index.Index(doc.ID, doc)
}

func (e *Engine) Search(queryString string) ([]string, error) {
	query := bleve.NewMatchQuery(queryString)
	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.Size = 3 // Retrieve top 3 relevant chunks

	searchResult, err := e.index.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	e.logger.Debugf("Search results for query '%s': %+v", queryString, searchResult)
	var contexts []string
	for _, hit := range searchResult.Hits {
		rawDoc, err := e.index.Document(hit.ID)
		if err != nil || rawDoc == nil {
			e.logger.Warnf("Failed to retrieve document for hit ID %s: %v\n full hit: %v\n\n", hit.ID, err, hit)
			continue
		}
		for _, field := range rawDoc.Fields {
			if field.Name() == "content" {
				contexts = append(contexts, string(field.Value()))
			}
		}
	}
	return contexts, nil
}
