"""
Search Service - Semantic search for notes using ChromaDB

This service provides gRPC API for indexing and searching notes.
It uses ChromaDB for vector storage and sentence-transformers for embeddings.

For a full MemPalace integration, you could replace this with direct calls to
mempalace's searcher module, but this provides more control and is simpler.
"""

import os
import time
from pathlib import Path
from datetime import datetime

import grpc
from concurrent import futures
import chromadb
from chromadb.config import Settings
from sentence_transformers import SentenceTransformer

import search_pb2
import search_pb2_grpc


class SearchService(search_pb2_grpc.SearchServiceServicer):
    """gRPC service for semantic note search."""

    def __init__(self, data_dir: str = "/data"):
        self.data_dir = data_dir
        self.notes_path = None
        
        # Initialize ChromaDB
        self.chroma_client = chromadb.Client(Settings(
            persist_directory=data_dir,
            anonymized_telemetry=False
        ))
        
        # Initialize embeddings model
        # Using a lightweight model for faster processing
        # For production, consider: "all-MiniLM-L6-v2" or "BAAI/bge-small-en-v1.5"
        self.embedder = SentenceTransformer("all-MiniLM-L6-v2")
        
        # Index metadata
        self._indexed_count = 0
        self._last_indexed_at = 0
        
        # Try to load existing collection
        try:
            self.collection = self.chroma_client.get_collection("notes")
            # Get count from ChromaDB
            self._indexed_count = self.collection.count()
        except Exception:
            # Create new collection
            self.collection = self.chroma_client.create_collection(
                name="notes",
                metadata={"description": "Notes semantic search index"}
            )

    def IndexNotes(self, request, context):
        """Index all markdown notes from the specified directory."""
        notes_path = request.notes_path
        
        if not notes_path:
            return search_pb2.IndexNotesResponse(
                success=False,
                indexed_count=0,
                error="notes_path is required"
            )
        
        if not os.path.isdir(notes_path):
            return search_pb2.IndexNotesResponse(
                success=False,
                indexed_count=0,
                error=f"Directory not found: {notes_path}"
            )
        
        self.notes_path = notes_path
        
        # Collect all markdown files
        md_files = list(Path(notes_path).rglob("*.md"))
        
        if not md_files:
            return search_pb2.IndexNotesResponse(
                success=True,
                indexed_count=0,
                error=None
            )
        
        # Prepare for indexing
        documents = []
        metadatas = []
        ids = []
        
        for md_file in md_files:
            try:
                content = md_file.read_text(encoding="utf-8")
                # Use filename (date) as ID
                file_id = md_file.stem
                
                documents.append(content)
                metadatas.append({
                    "date": file_id,
                    "path": str(md_file)
                })
                ids.append(file_id)
                
            except Exception as e:
                print(f"Error reading {md_file}: {e}")
                continue
        
        if not documents:
            return search_pb2.IndexNotesResponse(
                success=True,
                indexed_count=0,
                error="No readable markdown files found"
            )
        
        # Generate embeddings
        print(f"Generating embeddings for {len(documents)} notes...")
        embeddings = self.embedder.encode(documents).tolist()
        
        # Upsert to ChromaDB
        self.collection.upsert(
            ids=ids,
            documents=documents,
            embeddings=embeddings,
            metadatas=metadatas
        )
        
        self._indexed_count = len(ids)
        self._last_indexed_at = int(time.time())
        
        return search_pb2.IndexNotesResponse(
            success=True,
            indexed_count=len(ids),
            error=None
        )

    def SearchNotes(self, request, context):
        """Search notes using semantic query."""
        query = request.query
        limit = request.limit if request.limit > 0 else 5
        
        if not query:
            return search_pb2.SearchNotesResponse(results=[])
        
        # Generate query embedding
        query_embedding = self.embedder.encode([query]).tolist()[0]
        
        # Search in ChromaDB
        results = self.collection.query(
            query_embeddings=[query_embedding],
            n_results=limit
        )
        
        search_results = []
        
        if results["documents"] and results["documents"][0]:
            for i, doc in enumerate(results["documents"][0]):
                metadata = results["metadatas"][0][i] if results["metadatas"] else {}
                distance = results["distances"][0][i] if results["distances"] else 0
                
                # Convert distance to score (lower distance = higher score)
                score = 1.0 - distance
                
                search_results.append(search_pb2.SearchNote(
                    date=metadata.get("date", ""),
                    content=doc[:500],  # Limit content length
                    score=score
                ))
        
        return search_pb2.SearchNotesResponse(results=search_results)

    def GetIndexStatus(self, request, context):
        """Get current index status."""
        return search_pb2.GetIndexStatusResponse(
            indexed_count=self._indexed_count,
            last_indexed_at=self._last_indexed_at
        )


def serve(port: int = 50054, data_dir: str = "/data"):
    """Start the gRPC server."""
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    search_pb2_grpc.add_SearchServiceServicer_to_server(
        SearchService(data_dir=data_dir),
        server
    )
    
    server.add_insecure_port(f"[::]:{port}")
    server.start()
    
    print(f"Search service started on port {port}")
    print(f"Data directory: {data_dir}")
    
    server.wait_for_termination()


if __name__ == "__main__":
    import argparse
    
    parser = argparse.ArgumentParser(description="Notes Search Service")
    parser.add_argument("--port", type=int, default=50054, help="gRPC port")
    parser.add_argument("--data-dir", type=str, default="/data", help="ChromaDB data directory")
    
    args = parser.parse_args()
    
    serve(port=args.port, data_dir=args.data_dir)
