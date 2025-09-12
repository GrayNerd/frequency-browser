import json
import re

def parse_data_file(filename):
    """
    Parse the data file where everything after the site abbreviation is the site name.
    Handles site abbreviations that can contain digits as the second character.
    """
    entries = []
    current_country = None
    
    with open(filename, 'r', encoding='utf-8') as file:
        for line_num, line in enumerate(file, 1):
            line = line.strip()
            if not line:
                continue
                
            # Check if line contains a country abbreviation (1-4 uppercase letters followed by :)
            country_match = re.search(r'^([A-Z]{1,4}):', line)
            if country_match:
                current_country = country_match.group(1)
                # Remove the country part from the line
                line = line[country_match.end():].strip()
                print(f"Found country: {current_country}")
            
            # If no country found in this line, use previous country
            if current_country is None:
                print(f"Warning: Line {line_num} has no country and no previous country: {line}")
                continue
                
            # Process the entire line as site information
            process_site_line(line, current_country, entries, line_num)
    
    return entries

def process_site_line(line, country, entries, line_num):
    """
    Process a line to extract site information.
    Everything after the site abbreviation is the site name.
    Site abbreviations can contain digits as the second character.
    """
    # Skip exception lines
    if line.lower() in ['except:', 'except']:
        return
        
    # Check if line starts with a site abbreviation (1-2 alphanumeric characters followed by -)
    # Changed from [a-z]{1,2} to [a-z0-9]{1,2} to include digits
    abbrev_match = re.match(r'^([a-z0-9]{1,2})-(.+)$', line)
    
    if abbrev_match:
        site_abbrev = abbrev_match.group(1)
        site_name = abbrev_match.group(2).strip()
        print(f"  Added site with abbrev: {country}: {site_abbrev}-{site_name}")
    else:
        site_abbrev = ""
        site_name = line.strip()
        print(f"  Added site without abbrev: {country}: {site_name}")
        
    # Create entry
    entries.append({
        "country": country,
        "site_abbrev": site_abbrev,
        "site_name": site_name
    })

def create_json_file(data, output_filename):
    """
    Create a JSON file from the parsed data.
    """
    with open(output_filename, 'w', encoding='utf-8') as json_file:
        json.dump(data, json_file, indent=2, ensure_ascii=False)

def main():
    input_filename = "data.txt"
    output_filename = "sites.json"
    
    try:
        print("Parsing data file (site abbrev can contain digits)...")
        parsed_data = parse_data_file(input_filename)
        
        print(f"\nFound {len(parsed_data)} entries")
        
        # Count countries
        countries = set(entry['country'] for entry in parsed_data)
        print(f"Found {len(countries)} countries: {sorted(countries)}")
        
        # Show site abbreviations that contain digits
        digit_abbrevs = [entry for entry in parsed_data if any(char.isdigit() for char in entry['site_abbrev'])]
        if digit_abbrevs:
            print(f"\nSite abbreviations containing digits: {len(digit_abbrevs)}")
            for entry in digit_abbrevs[:10]:  # Show first 10
                print(f"  {entry['country']}: {entry['site_abbrev']}-{entry['site_name']}")
        
        print("Creating JSON file...")
        create_json_file(parsed_data, output_filename)
        
        print(f"Successfully created {output_filename}")
        
    except FileNotFoundError:
        print(f"Error: File '{input_filename}' not found.")
    except Exception as e:
        print(f"Error: {e}")

if __name__ == "__main__":
    main()
