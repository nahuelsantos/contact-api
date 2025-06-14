#!/usr/bin/env python3
"""
Contact API Test Script
Tests the contact API endpoints to ensure they're working properly.
"""

import requests
import json
import sys
from datetime import datetime

# Configuration
API_BASE_URL = "http://localhost:3002"  # Change this to your API URL
# API_BASE_URL = "http://contact-api:3002"  # Use this if testing from another container

def test_health_check():
    """Test the main health check endpoint"""
    print("🔍 Testing health check...")
    try:
        response = requests.get(f"{API_BASE_URL}/health", timeout=10)
        if response.status_code == 200:
            data = response.json()
            print(f"✅ Health check passed: {data['message']}")
            return True
        else:
            print(f"❌ Health check failed: {response.status_code}")
            return False
    except Exception as e:
        print(f"❌ Health check error: {e}")
        return False

def test_website_health_check(website):
    """Test website-specific health check"""
    print(f"🔍 Testing {website} health check...")
    try:
        response = requests.get(f"{API_BASE_URL}/api/v1/contact/{website}/health", timeout=10)
        if response.status_code == 200:
            data = response.json()
            print(f"✅ {website} health check passed")
            print(f"   Recipient: {data['data']['recipient']}")
            print(f"   SMTP Host: {data['data']['smtp_host']}")
            return True
        else:
            print(f"❌ {website} health check failed: {response.status_code}")
            return False
    except Exception as e:
        print(f"❌ {website} health check error: {e}")
        return False

def test_contact_form(website, test_name="Test"):
    """Test contact form submission"""
    print(f"📧 Testing {website} contact form ({test_name})...")
    
    contact_data = {
        "name": f"Test User ({test_name})",
        "email": "test@example.com",
        "subject": f"Test Contact Form - {website} - {test_name}",
        "message": f"This is a test message from the API test script.\n\nWebsite: {website}\nTest: {test_name}\nTime: {datetime.now().isoformat()}"
    }
    
    try:
        response = requests.post(
            f"{API_BASE_URL}/api/v1/contact/{website}",
            json=contact_data,
            headers={"Content-Type": "application/json"},
            timeout=30
        )
        
        if response.status_code == 200:
            data = response.json()
            if data.get('success'):
                print(f"✅ {website} contact form sent successfully")
                print(f"   Message: {data['message']}")
                return True
            else:
                print(f"❌ {website} contact form failed: {data.get('message', 'Unknown error')}")
                return False
        else:
            print(f"❌ {website} contact form failed: HTTP {response.status_code}")
            try:
                error_data = response.json()
                print(f"   Error: {error_data.get('message', 'Unknown error')}")
            except:
                print(f"   Response: {response.text}")
            return False
            
    except Exception as e:
        print(f"❌ {website} contact form error: {e}")
        return False

def test_invalid_data():
    """Test with invalid data to ensure validation works"""
    print("🧪 Testing validation with invalid data...")
    
    invalid_data = {
        "name": "",  # Missing required field
        "email": "invalid-email",  # Invalid email format
        "subject": "",  # Missing required field
        "message": ""  # Missing required field
    }
    
    try:
        response = requests.post(
            f"{API_BASE_URL}/api/v1/contact/test",
            json=invalid_data,
            headers={"Content-Type": "application/json"},
            timeout=10
        )
        
        if response.status_code == 400:
            print("✅ Validation working correctly (rejected invalid data)")
            return True
        else:
            print(f"❌ Validation failed: Expected 400, got {response.status_code}")
            return False
            
    except Exception as e:
        print(f"❌ Validation test error: {e}")
        return False

def main():
    """Run all tests"""
    print("🚀 Starting Contact API Tests")
    print(f"📍 Testing API at: {API_BASE_URL}")
    print("=" * 50)
    
    results = []
    
    # Test health checks
    results.append(test_health_check())
    results.append(test_website_health_check("nahuelsantos"))
    results.append(test_website_health_check("loopingbyte"))
    
    # Test contact forms for both websites
    results.append(test_contact_form("nahuelsantos", "Main Site"))
    results.append(test_contact_form("loopingbyte", "LoopingByte Site"))
    
    # Test validation
    results.append(test_invalid_data())
    
    # Summary
    print("\n" + "=" * 50)
    print("📊 Test Results Summary:")
    passed = sum(results)
    total = len(results)
    
    if passed == total:
        print(f"✅ All tests passed! ({passed}/{total})")
        print("🎉 Your Contact API is working perfectly!")
        sys.exit(0)
    else:
        print(f"❌ Some tests failed: {passed}/{total} passed")
        print("🔧 Please check the API configuration and logs")
        sys.exit(1)

if __name__ == "__main__":
    # Check if requests is available
    try:
        import requests
    except ImportError:
        print("❌ Error: 'requests' library not found")
        print("📦 Install it with: pip install requests")
        sys.exit(1)
    
    main() 