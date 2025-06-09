#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo "=== OverwriteBatch Basic Example Test ==="

# Create test directories
TEST_DIR="/tmp/overwritebatch-test-$$"
SOURCE_DIR="$TEST_DIR/source"
DEST_DIR="$TEST_DIR/dest"
STATUS_DB="$TEST_DIR/status.db"
BACKLOG_FILE="$TEST_DIR/backlog.gz"

echo "Creating test environment in $TEST_DIR..."
mkdir -p "$SOURCE_DIR"/{docs,logs,temp}
mkdir -p "$DEST_DIR"

# Create test files
echo "Creating test files..."
echo "This is a text file" > "$SOURCE_DIR/file1.txt"
echo "Another text file" > "$SOURCE_DIR/file2.txt"
echo "Log entry 1" > "$SOURCE_DIR/logs/app.log"
echo "Log entry 2" > "$SOURCE_DIR/logs/error.log"
echo "# Documentation" > "$SOURCE_DIR/docs/readme.md"
echo "Temporary file" > "$SOURCE_DIR/temp/cache.tmp"
echo "Config data" > "$SOURCE_DIR/config.conf"

# Build the example
echo "Building example..."
go build -o basic-example

# Test 1: Basic run with default patterns
echo -e "\n${GREEN}Test 1: Basic run with default patterns${NC}"
./basic-example -source "$SOURCE_DIR" -dest "$DEST_DIR" -status "$STATUS_DB" -backlog "$BACKLOG_FILE"

# Verify files were processed
if [ -f "$DEST_DIR/file1.txt" ] && [ -f "$DEST_DIR/file2.txt" ] && [ -f "$DEST_DIR/logs/app.log" ]; then
    echo -e "${GREEN}✓ Files processed successfully${NC}"
else
    echo -e "${RED}✗ Some files were not processed${NC}"
    exit 1
fi

# Verify timestamp header was added
if grep -q "# Processed by OverwriteBatch" "$DEST_DIR/file1.txt"; then
    echo -e "${GREEN}✓ Timestamp header added${NC}"
else
    echo -e "${RED}✗ Timestamp header not found${NC}"
    exit 1
fi

# Test 2: Run again to test deduplication
echo -e "\n${GREEN}Test 2: Testing deduplication (should skip already processed files)${NC}"
./basic-example -source "$SOURCE_DIR" -dest "$DEST_DIR" -status "$STATUS_DB" -backlog "$BACKLOG_FILE"

# Test 3: Uppercase transformation
echo -e "\n${GREEN}Test 3: Uppercase transformation${NC}"
rm -rf "$DEST_DIR"/*
rm -rf "$STATUS_DB"
./basic-example -source "$SOURCE_DIR" -dest "$DEST_DIR" -status "$STATUS_DB" -backlog "$BACKLOG_FILE" -uppercase

# Verify uppercase transformation
CONTENT=$(cat "$DEST_DIR/file1.txt")
if [[ "$CONTENT" == "THIS IS A TEXT FILE" ]]; then
    echo -e "${GREEN}✓ Uppercase transformation successful${NC}"
else
    echo -e "${RED}✗ Uppercase transformation failed${NC}"
    exit 1
fi

# Test 4: Custom patterns
echo -e "\n${GREEN}Test 4: Custom include/exclude patterns${NC}"
rm -rf "$DEST_DIR"/*
rm -rf "$STATUS_DB"
./basic-example -source "$SOURCE_DIR" -dest "$DEST_DIR" -status "$STATUS_DB" -backlog "$BACKLOG_FILE" \
    -include "*.md,*.conf" -exclude "temp/*"

# Verify pattern filtering
if [ -f "$DEST_DIR/docs/readme.md" ] && [ -f "$DEST_DIR/config.conf" ] && [ ! -f "$DEST_DIR/file1.txt" ]; then
    echo -e "${GREEN}✓ Pattern filtering working correctly${NC}"
else
    echo -e "${RED}✗ Pattern filtering failed${NC}"
    exit 1
fi

# Test 5: Multiple workers
echo -e "\n${GREEN}Test 5: Testing with multiple workers${NC}"
rm -rf "$DEST_DIR"/*
rm -rf "$STATUS_DB"

# Create more files for concurrent processing
for i in {1..20}; do
    echo "Test file $i" > "$SOURCE_DIR/test$i.txt"
done

./basic-example -source "$SOURCE_DIR" -dest "$DEST_DIR" -status "$STATUS_DB" -backlog "$BACKLOG_FILE" \
    -workers 8 -include "test*.txt"

# Count processed files
PROCESSED_COUNT=$(find "$DEST_DIR" -name "test*.txt" | wc -l)
if [ "$PROCESSED_COUNT" -eq 20 ]; then
    echo -e "${GREEN}✓ All 20 files processed with concurrent workers${NC}"
else
    echo -e "${RED}✗ Only $PROCESSED_COUNT/20 files processed${NC}"
    exit 1
fi

# Cleanup
echo -e "\n${GREEN}Cleaning up test environment...${NC}"
rm -rf "$TEST_DIR"
rm -f basic-example

echo -e "\n${GREEN}All tests passed! ✓${NC}"