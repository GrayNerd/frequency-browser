import json
import re

def parse_language_file(filename):
    """
    Parse the language reference file and extract language codes and names.
    
    Args:
        filename (str): Path to the input file
        
    Returns:
        dict: Dictionary containing language codes and names
    """
    language_dict = {}
    
    try:
        with open(filename, 'r', encoding='utf-8') as file:
            for line in file:
                # Skip comment lines and empty lines
                if line.strip().startswith(';') or not line.strip():
                    continue
                
                # Use regex to split on whitespace (but keep multi-word names together)
                # The pattern looks for: code(s) + language name (which may contain spaces)
                match = re.match(r'^([A-Za-z]{2,3}(?:\s+[A-Za-z]{2,3})*)\s+(.+)$', line.strip())
                
                if match:
                    codes_part = match.group(1)
                    language_name = match.group(2).strip()
                    
                    # Split multiple codes (if present)
                    codes = codes_part.split()
                    
                    # Add each code to the dictionary (convert to uppercase)
                    for code in codes:
                        language_dict[code.upper()] = language_name
                        
    except FileNotFoundError:
        print(f"Error: File '{filename}' not found.")
        return {}
    except Exception as e:
        print(f"Error reading file: {e}")
        return {}
    
    return language_dict

def create_json_output(language_dict, output_filename):
    """
    Create a JSON file from the language dictionary.
    
    Args:
        language_dict (dict): Dictionary containing language codes and names
        output_filename (str): Path for the output JSON file
    """
    try:
        with open(output_filename, 'w', encoding='utf-8') as json_file:
            json.dump(language_dict, json_file, indent=2, ensure_ascii=False)
        print(f"JSON file created successfully: {output_filename}")
    except Exception as e:
        print(f"Error writing JSON file: {e}")

def main():
    input_filename = "language.txt"  # Change this to your input file path
    output_filename = "language.json"
    
    # Parse the language file
    language_dict = parse_language_file(input_filename)
    
    if language_dict:
        # Create JSON output
        create_json_output(language_dict, output_filename)
        
        # Print sample output
        print("\nSample of the generated dictionary:")
        sample_keys = list(language_dict.keys())[:10]  # Show first 10 entries
        for key in sample_keys:
            print(f'  "{key}": "{language_dict[key]}"')
            
        print(f"\nTotal entries: {len(language_dict)}")
    else:
        print("No language data was parsed.")

if __name__ == "__main__":
    main()