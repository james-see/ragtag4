import os
import sys
import requests
from bs4 import BeautifulSoup
import argparse

# Default URL of the script to download
DEFAULT_SCRIPT_URL = "https://www.dailyscript.com/scripts/twelve_monkeys.html"

# Directory to save the downloaded script
save_dir = "scripts"
os.makedirs(save_dir, exist_ok=True)

# Function to download a script
def download_script(script_url, save_dir):
    response = requests.get(script_url)
    response.raise_for_status()

    # Parse the HTML content
    soup = BeautifulSoup(response.text, "html.parser")

    # Try to find the <pre> tag
    pre_tag = soup.find("pre")

    if pre_tag:
        # Extract the text content from the <pre> tag
        text_content = pre_tag.get_text()
        # Remove the line numbers
        lines = text_content.split("\n")
        cleaned_lines = [line.split("|", 1)[-1] for line in lines]
        cleaned_text = "\n".join(cleaned_lines)
    else:
        # If no <pre> tag, get the text from the body
        body = soup.find("body")
        if body:
            cleaned_text = body.get_text()
        else:
            raise ValueError("Could not find script content in the HTML")

    # Extract the filename from the URL and change the extension to .txt
    filename = os.path.basename(script_url).split(".")[0] + ".txt"
    save_path = os.path.join(save_dir, filename)

    with open(save_path, "w", encoding="utf-8") as file:
        file.write(cleaned_text)

    print(f"Downloaded: {save_path}")
    return save_path

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Download a screenplay from a given URL.")
    parser.add_argument("--url", type=str, default=DEFAULT_SCRIPT_URL,
                        help="URL of the screenplay to download (default: Twelve Monkeys)")
    
    args = parser.parse_args()
    script_url = args.url

    # Extract the script name from the URL
    script_name = os.path.basename(script_url).split(".")[0] + ".txt"
    save_path = os.path.join(save_dir, script_name)

    # Download the script
    download_script(script_url, save_dir)