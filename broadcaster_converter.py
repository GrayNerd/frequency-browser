#!/usr/bin/env python3
"""
Broadcaster Data Converter
Converts fixed-width broadcaster data to JSON format
"""

import json
import sys
import argparse
from typing import List, Dict

def parse_broadcaster_data(lines: List[str]) -> List[Dict]:
    """
    Parse fixed-width broadcaster data and convert to structured format
    
    Args:
        lines: List of lines from the input file
        
    Returns:
        List of dictionaries containing broadcaster information
    """
    broadcasters = []
    
    for line in lines:
        # Skip comment lines and empty lines
        if line.startswith(';') or line.strip() == '':
            continue
            
        # Extract code (first 3 characters)
        code = line[:3].strip()
        
        # Extract broadcaster name (rest of the line after code)
        broadcaster_name = line[3:].strip()
        
        # Skip lines without proper data
        if not code or not broadcaster_name:
            continue
            
        broadcasters.append({
            'code': code,
            'broadcaster_name': broadcaster_name
        })
    
    return broadcasters

def convert_to_json(input_file: str, output_file: str):
    """
    Convert fixed-width broadcaster data to JSON format
    
    Args:
        input_file: Path to input text file
        output_file: Path to output JSON file
    """
    try:
        # Read input file
        with open(input_file, 'r', encoding='utf-8') as f:
            lines = f.readlines()
        
        # Parse the data
        broadcasters = parse_broadcaster_data(lines)
        
        # Write to JSON file
        with open(output_file, 'w', encoding='utf-8') as f:
            json.dump(broadcasters, f, indent=2, ensure_ascii=False)
        
        print(f"Successfully converted {len(broadcasters)} broadcasters to {output_file}")
        
    except FileNotFoundError:
        print(f"Error: Input file '{input_file}' not found.")
        sys.exit(1)
    except Exception as e:
        print(f"Error: {e}")
        sys.exit(1)

def main():
    """Main function to handle command line arguments and execution"""
    parser = argparse.ArgumentParser(
        description='Convert fixed-width broadcaster data to JSON format',
        epilog='Example: python broadcaster_converter.py broadcasters.txt broadcasters.json'
    )
    
    parser.add_argument('input_file', help='Input text file with broadcaster data')
    parser.add_argument('output_file', help='Output JSON file')
    
    args = parser.parse_args()
    
    print(f"Converting {args.input_file} to {args.output_file}...")
    convert_to_json(args.input_file, args.output_file)

if __name__ == "__main__":
    main()
