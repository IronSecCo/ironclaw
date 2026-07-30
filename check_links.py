import re
from pathlib import Path
import sys

# 1. Inline links: [text](url)
INLINE_LINK_REGEX = re.compile(r'\[([^\]]+)\]\((?!http|https|mailto)(.*?)\)')

# 2. Reference definitions: [ref]: url
REF_DEF_REGEX = re.compile(
    r'^\s*\[([^\]]+)\]:\s*(?!http|https|mailto)(\.?\/?[\w\-\.\/]+(?:#[\w\-\.]*)?|#[\w\-\.]+)(?:\s+["\'].*?["\'])?\s*$',
    re.MULTILINE
)

# 3. HTML hrefs: href="url"
HTML_HREF_REGEX = re.compile(r'href=["\'](?!http|https|mailto)([^"\']+)["\']', re.IGNORECASE)

# 4. Soft Link Rot check: Flags links referencing sections that omit anchor fragments
SECTION_REF_REGEX = re.compile(r'[\§]|(?:\b(?:section|part)\s+\d+)', re.IGNORECASE)

# Headings (including list-nested headings)
HEADING_REGEX = re.compile(r'^(?:\s*[\-\*\+]\s+)?(#+)\s+(.*)$', re.MULTILINE)
ATTR_ID_REGEX = re.compile(r'\{\s*#([a-zA-Z0-9\-_]+)\s*\}')
HTML_ANCHOR_REGEX = re.compile(r'<(?:[a-zA-Z0-9\-]+)[^>]+(?:id|name)=["\']([^"\']+)["\']', re.IGNORECASE)

def slugify(text):
    text = re.sub(r':[\w\-]+:', '', text)
    text = re.sub(r'\{[^}]*\}', '', text)
    text = text.lower()
    text = re.sub(r'[^\w\s-]', '', text)
    text = re.sub(r'\s+', '-', text)
    return text.strip('-')

def get_file_headings(filepath):
    if not filepath.exists():
        return set()
    
    with open(filepath, 'r', encoding='utf-8') as f:
        content = f.read()
        
    anchors = set()
    for match in HEADING_REGEX.finditer(content):
        raw_heading = match.group(2)
        attr_match = ATTR_ID_REGEX.search(raw_heading)
        if attr_match:
            anchors.add(attr_match.group(1))
        anchors.add(slugify(raw_heading))
        
    for match in HTML_ANCHOR_REGEX.finditer(content):
        anchors.add(match.group(1))
        
    return anchors

def check_documentation():
    root_dir = Path('.')
    target_files = [root_dir / 'README.md'] + list((root_dir / 'docs').rglob('*.md'))
    
    errors_found = False

    for file_path in target_files:
        if not file_path.exists():
            continue

        with open(file_path, 'r', encoding='utf-8') as f:
            content = f.read()

        target_urls = []

        for m in INLINE_LINK_REGEX.finditer(content):
            target_urls.append((m.group(1), m.group(2), "Inline link"))

        for m in REF_DEF_REGEX.finditer(content):
            target_urls.append((m.group(1), m.group(2), "Reference link"))

        for m in HTML_HREF_REGEX.finditer(content):
            target_urls.append(("HTML", m.group(1), "HTML href"))

        for link_text, link_url, link_type in target_urls:
            if file_path.name == 'scan-coverage.md' and link_url == '<deep doc>':
                continue

            link_url = link_url.split()[0] if link_url else ''

            if '#' in link_url:
                target_path_str, fragment = link_url.split('#', 1)
            else:
                target_path_str, fragment = link_url, None

            # Soft Link Rot Warning
            if not fragment and SECTION_REF_REGEX.search(link_text):
                print(f"⚠️ [Missing Anchor Warning] ({link_type}) in {file_path.name}: "
                      f"Link text '{link_text}' references a section, but target '{link_url}' lacks an anchor fragment.")

            if target_path_str == '':
                target_file = file_path
            else:
                target_file = (file_path.parent / target_path_str).resolve()

            if not target_file.exists():
                print(f"❌ [File Missing] ({link_type}) in {file_path.name}: '{target_path_str}' does not exist.")
                errors_found = True
                continue

            if fragment:
                valid_slugs = get_file_headings(target_file)
                if fragment not in valid_slugs:
                    print(f"❌ [Anchor Broken] ({link_type}) in {file_path.name}: "
                          f"Heading '#{fragment}' not found in {target_file.name}")
                    errors_found = True

    if errors_found:
        print("\n Link check failed. Please fix the broken links above.")
        sys.exit(1)
    else:
        print("\n✅ All relative links and anchor fragments are valid!")
        sys.exit(0)

if __name__ == '__main__':
    check_documentation()
