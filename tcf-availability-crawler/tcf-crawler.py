import time
import csv
import signal
from queue import Queue
import requests
import concurrent.futures
from selenium import webdriver
from selenium.webdriver.chrome.options import Options
from selenium.common.exceptions import TimeoutException, WebDriverException
from threading import Lock, BoundedSemaphore
from tenacity import retry, stop_after_attempt, wait_exponential
import logging

logging.basicConfig(level=logging.INFO)


class Settings:
    """
    A class used to encapsulate various settings related to a WebDriver and CSV handling.

    Attributes
    ----------
    driver_pool_size : int
        The number of WebDrivers in the pool.
    chrome_args : list
        The command-line arguments to be passed to the Chrome browser.
    chrome_prefs : dict
        The preferences to be set for the Chrome browser.
    page_load_timeout : int
        The maximum time, in seconds, that the WebDriver will wait for a page to load.
    batch_size : int
        The number of rows to be kept in memory before they are written to the CSV file.
    tcf_wait_time : int
        The time, in seconds, to wait before checking if the TCF API is available.
    tcf_wait_interval : int
        The interval, in seconds, between consecutive checks for TCF API availability.

    Methods
    -------
    __init__(self, driver_pool_size, chrome_args, chrome_prefs, page_load_timeout, batch_size, tcf_wait_time, tcf_wait_interval)
        Initializes the Settings instance with the specified settings.
    """

    def __init__(self, driver_pool_size, chrome_args, chrome_prefs, page_load_timeout, batch_size, tcf_wait_time, tcf_wait_interval):
        self.driver_pool_size = driver_pool_size
        self.chrome_args = chrome_args
        self.chrome_prefs = chrome_prefs
        self.page_load_timeout = page_load_timeout
        self.batch_size = batch_size
        self.tcf_wait_time = tcf_wait_time
        self.tcf_wait_interval = tcf_wait_interval

class WebDriverPool:
    """
    A class used to represent a pool of Selenium WebDriver instances. 

    Attributes
    ----------
    size : int
        Number of WebDrivers in the pool.
    semaphore : BoundedSemaphore
        A semaphore to manage concurrent access to WebDriver instances.
    pool : Queue
        The pool where WebDriver instances are stored.
    settings : Settings
        A Settings object containing various parameters.

    Methods
    -------
    initialize_drivers():
        Populates the WebDriver pool with driver instances.
    create_and_add_driver():
        Creates a new WebDriver instance and adds it to the pool.
    get_driver():
        Retrieves a WebDriver instance from the pool, if available.
    return_driver(driver: webdriver):
        Returns a WebDriver instance to the pool.
    quit_all_drivers():
        Closes all WebDriver instances and empties the pool.
    """
    def __init__(self, settings):
        """Initializes WebDriverPool with given settings."""
        self.size = settings.driver_pool_size
        self.semaphore = BoundedSemaphore(self.size)
        self.pool = Queue(self.size)
        self.settings = settings
        self.initialize_drivers()

    def __enter__(self):
        """Ensures the pool can be used in a 'with' statement."""
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        """Ensures all drivers quit when leaving a 'with' statement context."""
        self.quit_all_drivers()

    def initialize_drivers(self):
        """Creates and adds WebDriver instances to the pool according to the pool size."""
        for _ in range(self.size):
            self.create_and_add_driver()

    def create_and_add_driver(self):
        """Creates a new WebDriver instance with the specified Chrome options and adds it to the pool."""
        chrome_options = Options()
        for arg in self.settings.chrome_args:
            chrome_options.add_argument(arg)
        chrome_options.add_experimental_option("prefs", self.settings.chrome_prefs)
        driver = webdriver.Chrome(options=chrome_options)
        driver.set_page_load_timeout(self.settings.page_load_timeout)
        self.pool.put(driver)

    def get_driver(self):
        """
        Retrieves a WebDriver instance from the pool. If no instance is available, 
        this method will block until one becomes available.
        """
        self.semaphore.acquire()
        return self.pool.get()

    def return_driver(self, driver: webdriver):
        """
        Returns a WebDriver instance to the pool, replacing it if it's broken.
        
        If a WebDriverException is raised when accessing the instance, it is quit and replaced by a new one.
        """
        try:
            driver.get("about:blank")
            self.pool.put(driver)
        except WebDriverException:
            logging.error("Error in WebDriver, restarting it.")
            driver.quit()
            self.create_and_add_driver()
        finally:
            self.semaphore.release()

    def quit_all_drivers(self):
        """Quits all WebDriver instances in the pool and removes them from the pool."""
        while not self.pool.empty():
            driver = self.pool.get()
            driver.quit()


class Scraper:
    """
    A class used to scrape a given domain and store the results.

    Attributes
    ----------
    session : Session
        A requests Session object for making HTTP requests.
    driver_pool : WebDriverPool
        A pool of WebDriver instances for accessing websites.
    driver : WebDriver
        A WebDriver instance for this scraper.
    domain : str
        The domain that this scraper is tasked with scraping.
    writer : _csv.writer
        A CSV writer for outputting the scrape results.
    lock : Lock
        A threading Lock for controlling access to shared resources.
    data_buffer : list
        A buffer for storing data before it is written to the CSV file.
    settings : Settings
        A Settings object containing various parameters.

    Methods
    -------
    flush_data_buffer():
        Writes the contents of the data buffer to the CSV file and clears the buffer.
    add_to_data_buffer(data):
        Adds a piece of data to the data buffer and flushes the buffer if it is full.
    load_url(url):
        Loads a URL using the WebDriver. If loading fails, retries up to 3 times.
    scan_domain():
        Scans the domain, records whether the TCF API is available, and writes the results to the CSV file.
    flush_data_buffer_if_needed():
        Flushes the data buffer if it is full.
    """
    def __init__(self, domain, writer, lock, web_driver_pool, settings):
        """Initializes Scraper with given domain, writer, lock, WebDriverPool, and settings."""
        self.session = requests.Session()
        self.driver_pool = web_driver_pool
        self.driver = None
        self.domain = domain
        self.writer = writer
        self.lock = lock
        self.data_buffer = []
        self.settings = settings

    def flush_data_buffer(self):
        """
        Writes the contents of the data buffer to the CSV file and clears the buffer.
        This operation is thread-safe due to the usage of a lock.
        """
        with self.lock:
            self.writer.writerows(self.data_buffer)
            self.data_buffer = []
        logging.info('Data buffer flushed.')

    def add_to_data_buffer(self, data):
        """
        Adds a piece of data to the data buffer and flushes the buffer if it is full.
        """
        self.data_buffer.append(data)
        if len(self.data_buffer) >= self.settings.batch_size:
            self.flush_data_buffer()

    @retry(stop=stop_after_attempt(3), wait=wait_exponential(multiplier=1, min=2, max= 10))
    def load_url(self, url):
        """
        Loads a URL using the WebDriver. If loading fails due to a timeout or WebDriver exception,
        retries up to 3 times with exponentially increasing wait times.
        """
        try:
            self.driver.get(url)
        except (TimeoutException, WebDriverException) as e:
            logging.error(f"Failed to load {url}, retrying...")
            raise

    def scan_domain(self):
        """
        Scans the domain, records whether the TCF API is available, and writes the results to the CSV file.
        """
        try:
            self.driver = self.driver_pool.get_driver()
            url = 'https://www.' + self.domain
            try:
                self.load_url(url)
            except (TimeoutException, WebDriverException) as e:
                self.add_to_data_buffer([self.domain, f'Error: {e}'])
                logging.error(f"{url}: Error: {e}")
                return

            start_time = time.time()
            tcf_available = False
            while time.time() - start_time < self.settings.tcf_wait_time:
                result = self.driver.execute_script('return typeof window.__tcfapi;')
                if result and 'function' in result:
                    tcf_available = True
                    break
                time.sleep(self.settings.tcf_wait_interval)

            if not tcf_available:
                self.add_to_data_buffer([self.domain, 'No'])
                print(f"{url}: TCF API is not available")
            else:
                self.add_to_data_buffer([self.domain, 'Yes'])
                print(f"{self.domain}: TCF API is available")

            self.flush_data_buffer_if_needed()
        except Exception as e:
            logging.error(f"Error while scanning domain {self.domain}: {e}")
            self.add_to_data_buffer([self.domain, f'Error: {e}'])
        finally:
            self.driver_pool.return_driver(self.driver)
            self.flush_data_buffer()

    def flush_data_buffer_if_needed(self):
        """
        Checks if the data buffer is full. If so, it calls the flush_data_buffer method to write the
        contents of the buffer to the CSV file and then clears the buffer.
        """
        with self.lock:
            if len(self.data_buffer) >= self.settings.batch_size:
                self.flush_data_buffer()


if __name__ == '__main__':

    DRIVER_POOL_SIZE = 3
    CHROME_ARGS = ["--ignore-certificate-errors", "--headless", "--disable-gpu"]
    CHROME_PREFS = {
        "profile.managed_default_content_settings.images": 2, 
        "permissions.default.stylesheet": 2, 
        "javascript.enabled": True
    }
    PAGE_LOAD_TIMEOUT = 60
    BATCH_SIZE = 100
    TCF_WAIT_TIME = 6
    TCF_WAIT_INTERVAL = 0.25
    INPUT_FILE = 'domains.csv'
    OUTPUT_FILE = 'results.csv'

    settings = Settings(
        driver_pool_size=DRIVER_POOL_SIZE,
        chrome_args=CHROME_ARGS,
        chrome_prefs=CHROME_PREFS,
        page_load_timeout=PAGE_LOAD_TIMEOUT,
        batch_size=BATCH_SIZE,
        tcf_wait_time=TCF_WAIT_TIME,
        tcf_wait_interval=TCF_WAIT_INTERVAL
    )

    with open(INPUT_FILE) as f, open(OUTPUT_FILE, 'w') as csvfile:
        reader = csv.reader(f)
        writer = csv.writer(csvfile, lineterminator='\n')
        writer.writerow(['Domain', 'TCF API Available'])
        csvfile.flush()

        lock = Lock()

        with WebDriverPool(settings) as web_driver_pool:
            def cleanup(signum, frame):
                logging.info('Caught signal, quitting all drivers...')
                web_driver_pool.quit_all_drivers()
                exit(0)

            signal.signal(signal.SIGINT, cleanup)
            signal.signal(signal.SIGTERM, cleanup)

            futures = {}
            with concurrent.futures.ThreadPoolExecutor() as executor:
                for row in reader:
                    domain = row[0]
                    scraper = Scraper(domain, writer, lock, web_driver_pool, settings)
                    futures[executor.submit(scraper.scan_domain)] = domain # store each Future object in a dictionary along with its corresponding domain.

            for future in concurrent.futures.as_completed(futures):
                domain = futures[future]
                try:
                    future.result() # Return the result of the future as soon as it is available
                except Exception as e:
                    logging.error(f"Error in worker thread for domain {domain}: {e}")
