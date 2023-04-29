import time
import csv
import requests
from selenium import webdriver
from selenium.webdriver.chrome.options import Options
from selenium.common.exceptions import TimeoutException
from multiprocessing import Pool, cpu_count

# Set up Selenium options
chrome_options = Options()
chrome_options.add_argument('--headless') # Run in headless mode

def scan_domain(domain):
    try:
        # Check if domain is reachable
        response = requests.get('https://' + domain, timeout=10)
        if response.status_code != 200:
            print(f"{domain}: Error: Domain is not reachable")
            return [domain, 'Error']
    except Exception as e:
        print(f"{domain}: Error: {e}")
        return [domain, 'Error']

    driver = None
    try:
        # Initialize webdriver
        driver = webdriver.Chrome(options=chrome_options)
        driver.set_page_load_timeout(30) # Set a timeout for the driver.get() method

        # Navigate to the domain
        try:
            driver.get("https://" + domain)
        except TimeoutException:
            print(f"{domain}: Error: Timeout")
            return [domain, 'Error']

        # Wait for TCF API to become available
        tcf_available = False
        for i in range(20):
            if 'function' in driver.execute_script('return typeof window.__tcfapi;'):
                tcf_available = True
                break
            time.sleep(0.1)
        if not tcf_available:
            print(f"{domain}: TCF API is not available")
            return [domain, 'No']

        print(f"{domain}: TCF API is available")
        return [domain, 'Yes']

    except Exception as e:
        print(f"{domain}: Error: {e}")
        return [domain, 'Error']

    finally:
        if driver is not None:
            driver.quit()

if __name__ == '__main__':
    with open('tranco_top-1m.csv') as f, open('results_run_3.csv', 'w', newline='') as csvfile:
        reader = csv.reader(f)
        writer = csv.writer(csvfile)
        writer.writerow(['Domain', 'TCF API Available']) # Write header row

        # Create a process pool with the number of CPUs
        pool = Pool(cpu_count())

        # Scan each domain in the list using the process pool
        for result in pool.imap_unordered(scan_domain, (row[1] for row in reader)):
            writer.writerow(result)

        pool.close()
        pool.join()