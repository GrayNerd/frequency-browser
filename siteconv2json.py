#!/usr/bin/env python3
"""
HF Transmitter Site Converter
Converts fixed-width format transmitter site data to JSON format
"""

import json
import sys
import argparse

def convert_to_json(input_file, output_file):
    """
    Convert fixed-width transmitter site data to JSON format
    
    Args:
        input_file (str): Path to input text file
        output_file (str): Path to output JSON file
    """
    data = []
    
    try:
        with open(input_file, 'r', encoding='utf-8') as f:
            lines = f.readlines()
            
            for line in lines:
                # Skip comment lines and header lines
                if line.startswith(';') or line.strip() == '':
                    continue
                    
                # Extract fields using fixed width positions
                # Code: positions 0-3 (4 characters)
                # Site Name: positions 4-33 (30 characters)
                # ADM: positions 34-37 (4 characters)
                code = line[0:4].strip()
                site_name = line[4:34].strip()
                adm = line[34:38].strip()
                
                # Only add entries with valid data
                if code and site_name and adm:
                    data.append({
                        'code': code,
                        'site_name': site_name,
                        'ADM': adm
                    })
        
        # Write to JSON file
        with open(output_file, 'w', encoding='utf-8') as f:
            json.dump(data, f, indent=2, ensure_ascii=False)
            
        print(f"Successfully converted {len(data)} records to {output_file}")
        
    except FileNotFoundError:
        print(f"Error: Input file '{input_file}' not found.")
        sys.exit(1)
    except Exception as e:
        print(f"Error: {e}")
        sys.exit(1)

def main():
    """Main function to handle command line arguments and execution"""
    parser = argparse.ArgumentParser(
        description='Convert fixed-width HF transmitter site data to JSON format',
        epilog='Example: python converter.py sites.txt sites.json'
    )
    
    parser.add_argument('input_file', help='Input text file with transmitter site data')
    parser.add_argument('output_file', help='Output JSON file')
    
    args = parser.parse_args()
    
    print(f"Converting {args.input_file} to {args.output_file}...")
    convert_to_json(args.input_file, args.output_file)

if __name__ == "__main__":
    main()