#!/bin/bash

# Comprehensive test script for gail
# Runs all test scenarios in sequence

LOG_FILE="test.log"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

print_header() {
    echo ""
    echo -e "${CYAN}=========================================${NC}"
    echo -e "${CYAN}  $1${NC}"
    echo -e "${CYAN}=========================================${NC}"
}

print_info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

# Main test sequence
print_header "gail Comprehensive Test Suite"
echo ""
echo "This script will test all gail features:"
echo "  • Basic follow mode"
echo "  • File truncation (${RED}RED T${NC} marker)"
echo "  • File recreation (${YELLOW}ORANGE R${NC}/${MAGENTA}MAGENTA X${NC}/${CYAN}CYAN M${NC} markers)"
echo "  • Horizontal scrolling (long lines)"
echo "  • Vertical scrolling and buffer"
echo ""
echo "Open another terminal and run: ${GREEN}./gail $LOG_FILE${NC}"
echo ""

# Phase 1: Initial file creation
print_header "Phase 1: Initial File Creation"
cat > $LOG_FILE << 'EOF'
Initial line 1
Initial line 2
Initial line 3
EOF
print_success "Created $LOG_FILE with 3 lines"
sleep 2

# Phase 2: Normal appends (green dots)
print_header "Phase 2: Normal Appends"
print_info "Adding lines with normal green dots..."
for i in {4..10}; do
    echo "Line $i" >> $LOG_FILE
    echo "  Added line $i"
    sleep 0.5
done
sleep 2

# Phase 3: First truncation (RED T marker)
print_header "Phase 3: File Truncation"
echo "Current file size: $(wc -c < $LOG_FILE) bytes"
print_info "Truncating file..."
true > $LOG_FILE
print_success "File truncated to 0 bytes"
echo ""
print_warning ">>> CHECK gail: You should see ${RED}RED 'T'${NC} marker <<<"
sleep 3

# Phase 4: Write after truncation
print_header "Phase 4: Writing After Truncation"
print_info "Adding lines to truncated file..."
for i in {1..5}; do
    echo "After truncate - Line $i" >> $LOG_FILE
    echo "  Added line $i"
    sleep 0.5
done
sleep 2

# Phase 5: File recreation (ORANGE R/MAGENTA X/CYAN M marker)
print_header "Phase 5: File Recreation"
print_info "Recreating file with new content..."
cat > $LOG_FILE << 'EOF'
NEW FILE - Line 1
NEW FILE - Line 2
NEW FILE - Line 3
EOF
print_success "File recreated"
echo ""
print_warning ">>> CHECK gail: You should see ${YELLOW}ORANGE 'R'${NC}, ${MAGENTA}MAGENTA 'X'${NC}, or ${CYAN}CYAN 'M'${NC} marker <<<"
sleep 3

# Phase 6: Append to recreated file
print_header "Phase 6: Appending to New File"
print_info "Adding lines to recreated file..."
for i in {4..10}; do
    echo "NEW FILE - Line $i" >> $LOG_FILE
    echo "  Added line $i"
    sleep 0.5
done
sleep 2

# Phase 7: Second truncation (another RED T marker)
print_header "Phase 7: Second Truncation"
print_info "Truncating file again..."
true > $LOG_FILE
print_success "File truncated again"
echo ""
print_warning ">>> CHECK gail: You should see another ${RED}RED 'T'${NC} marker <<<"
sleep 3

# Phase 8: Long lines for horizontal scrolling
print_header "Phase 8: Long Lines (Horizontal Scrolling)"
print_info "Adding long lines to test horizontal scrolling..."
for i in {1..5}; do
    timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    echo "[$timestamp] Long log entry $i - This is a very long line with lots of text to demonstrate horizontal scrolling functionality. You should be able to use arrow keys (left/right or h/l) to scroll and see the full line. The line will be truncated with ... when it's too long for the terminal width." >> $LOG_FILE
    echo "  Added long line $i"
    sleep 0.7
done
sleep 2

# Phase 9: More entries for vertical scrolling
print_header "Phase 9: Multiple Entries (Vertical Scrolling)"
print_info "Adding 20 more entries to test vertical scrolling..."
for i in {1..20}; do
    timestamp=$(date '+%H:%M:%S')
    echo "[$timestamp] Entry $i" >> $LOG_FILE
    if [ $((i % 5)) -eq 0 ]; then
        echo "  Added $i/20 entries"
    fi
    sleep 0.3
done
sleep 2

# Phase 10: Final rapid entries
print_header "Phase 10: Rapid Fire Entries"
print_info "Adding rapid entries to test fade effect..."
for i in {1..15}; do
    echo "Rapid entry $i" >> $LOG_FILE
    sleep 0.2
done
print_success "Added 15 rapid entries"
sleep 2

# Phase 11: Partial line buffering (OneDrive-style progress dots)
print_header "Phase 11: Partial Line Buffering"
print_info "Testing partial line writes (simulating OneDrive progress)..."
echo ""
print_warning ">>> CHECK gail: You should see ${YELLOW}◐${NC} indicator for partial lines <<<"
print_warning ">>> Dots should accumulate and update in real-time <<<"
echo ""
printf "Progress: " >> $LOG_FILE
sleep 1
for i in {1..10}; do
    printf "." >> $LOG_FILE
    echo "  Wrote dot $i (no newline)"
    sleep 0.3
done
echo " Done!" >> $LOG_FILE
echo "  Completed line with newline"
sleep 2

print_info "Adding another partial line test..."
printf "Loading" >> $LOG_FILE
sleep 0.5
for i in {1..5}; do
    printf "." >> $LOG_FILE
    echo "  Wrote dot $i (no newline)"
    sleep 0.4
done
printf " Complete\n" >> $LOG_FILE
echo "  Completed line with newline"
sleep 2

print_success "Partial line test complete"
echo ""
print_info "Expected behavior:"
echo "  • You should see ${YELLOW}◐ Progress:${NC} with amber indicator (partial line)"
echo "  • Dots accumulate: ${YELLOW}◐ Progress: .....${NC} (may batch due to timing)"
echo "  • When complete: ${GREEN}● Progress: .......... Done!${NC} (green dot)"
echo "  • Same pattern for 'Loading' test"
echo ""
echo "Note: With 0.3-0.4s delays, multiple dots may batch together before"
echo "      gail receives the file notification. This is normal OS behavior."
sleep 3

# Summary
print_header "TEST SUITE COMPLETE"
echo ""
echo "All tests executed. In gail you should have seen:"
echo ""
echo "Visual Indicators:"
echo "  • ${GREEN}Green dots (●)${NC} for normal lines that fade to gray"
echo "  • ${YELLOW}Amber half-circle (◐)${NC} for partial lines (incomplete, no newline yet)"
echo "  • ${RED}RED 'T'${NC} markers for truncations (appeared 2 times)"
echo "  • ${YELLOW}ORANGE 'R'${NC}/${MAGENTA}MAGENTA 'X'${NC}/${CYAN}CYAN 'M'${NC} marker for file recreation"
echo ""
echo "Functionality Tested:"
echo "  • Follow mode (auto-scrolling new content)"
echo "  • Vertical scrolling (up/down, pgup/pgdown, g/G)"
echo "  • Horizontal scrolling (left/right for long lines)"
echo "  • File truncation detection"
echo "  • File recreation handling"
echo "  • Fade effects on new lines"
echo "  • Partial line buffering with real-time updates (${YELLOW}◐${NC} indicator)"
echo "  • Status bar with position and last update time"
echo ""
echo "Try these in gail:"
echo "  • Scroll up with ↑ or k (disables follow mode)"
echo "  • Scroll down with ↓ or j"
echo "  • Press G or End to jump to bottom (re-enables follow mode)"
echo "  • Scroll left/right with ←/→ or h/l for long lines"
echo "  • Press q to quit"
echo ""
print_success "Test complete!"
