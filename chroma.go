package main

import (
	"context"
	"fmt"
	"log"

	chroma "github.com/amikos-tech/chroma-go/pkg/api/v2"
)

func process(ctx context.Context) {

	err := collection.Add(ctx,
		chroma.WithIDGenerator(chroma.NewULIDGenerator()),
		chroma.WithIDs("1", "2"),
		chroma.WithTexts("hello world", "goodbye world"),
		chroma.WithMetadatas(
			chroma.NewDocumentMetadata(chroma.NewIntAttribute("int", 1)),
			chroma.NewDocumentMetadata(chroma.NewStringAttribute("str", "hello")),
		))
	if err != nil {
		log.Fatalf("Error adding collection: %s \n", err)
	}

	count, err := collection.Count(context.Background())
	if err != nil {
		log.Fatalf("Error counting collection: %s \n", err)
		return
	}
	fmt.Printf("Count collection: %d\n", count)

	qr, err := collection.Query(context.Background(), chroma.WithQueryTexts("say hello"))
	if err != nil {
		log.Fatalf("Error querying collection: %s \n", err)
		return
	}
	fmt.Printf("Query result: %v\n", qr.GetDocumentsGroups()[0][0])

	err = collection.Delete(context.Background(), chroma.WithIDsDelete("1", "2"))
	if err != nil {
		log.Fatalf("Error deleting collection: %s \n", err)
		return
	}
}
