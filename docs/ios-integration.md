# iOS App Integration Guide

This guide shows how to update the Pulse iOS app to use the Supabase backend instead of the Guardian API.

## Overview

The integration follows the existing architecture patterns in Pulse:
- Create a new `SupabaseNewsService` implementing `NewsService` protocol
- No changes needed to ViewModels, Interactors, or Views
- Simply swap the service registration in `PulseSceneDelegate`

## Step 1: Add Supabase Swift SDK

Add to your `Package.swift` or via Xcode:

```swift
// In project.yml (XcodeGen) add:
packages:
  Supabase:
    url: https://github.com/supabase/supabase-swift
    from: 2.0.0
```

Or via SPM:
```
https://github.com/supabase/supabase-swift
```

## Step 2: Create Supabase Configuration

Create `Pulse/Configs/Networking/SupabaseConfig.swift`:

```swift
import Foundation

enum SupabaseConfig {
    // These use the anon (public) key - safe to include in app
    static var url: String {
        // Option 1: From Remote Config (recommended)
        if let url = RemoteConfigManager.shared.string(forKey: "supabase_url") {
            return url
        }
        // Option 2: From environment (for CI)
        if let url = ProcessInfo.processInfo.environment["SUPABASE_URL"] {
            return url
        }
        // Option 3: Fallback (replace with your actual URL)
        return "https://your-project.supabase.co"
    }

    static var anonKey: String {
        if let key = RemoteConfigManager.shared.string(forKey: "supabase_anon_key") {
            return key
        }
        if let key = ProcessInfo.processInfo.environment["SUPABASE_ANON_KEY"] {
            return key
        }
        // Replace with your actual anon key
        return "your-anon-key"
    }
}
```

## Step 3: Create Supabase Response Models

Create `Pulse/Home/API/SupabaseModels.swift`:

```swift
import Foundation

// MARK: - Supabase Article Response
struct SupabaseArticle: Codable {
    let id: String
    let title: String
    let summary: String?
    let content: String?
    let url: String
    let imageUrl: String?
    let thumbnailUrl: String?
    let author: String?
    let publishedAt: Date
    let createdAt: Date
    let sourceName: String?
    let sourceSlug: String?
    let sourceLogoUrl: String?
    let sourceWebsiteUrl: String?
    let categoryName: String?
    let categorySlug: String?

    enum CodingKeys: String, CodingKey {
        case id, title, summary, content, url, author
        case imageUrl = "image_url"
        case thumbnailUrl = "thumbnail_url"
        case publishedAt = "published_at"
        case createdAt = "created_at"
        case sourceName = "source_name"
        case sourceSlug = "source_slug"
        case sourceLogoUrl = "source_logo_url"
        case sourceWebsiteUrl = "source_website_url"
        case categoryName = "category_name"
        case categorySlug = "category_slug"
    }

    /// Convert to the app's Article model
    func toArticle() -> Article {
        Article(
            id: id,
            title: title,
            description: summary,
            content: content,
            url: url,
            imageURL: imageUrl ?? thumbnailUrl,
            publishedAt: publishedAt,
            source: Source(
                id: sourceSlug ?? "unknown",
                name: sourceName ?? "Unknown"
            ),
            author: author,
            category: categorySlug.flatMap { NewsCategory(rawValue: $0) } ?? .world
        )
    }
}

// MARK: - Supabase Category Response
struct SupabaseCategory: Codable {
    let id: String
    let name: String
    let slug: String
    let displayOrder: Int

    enum CodingKeys: String, CodingKey {
        case id, name, slug
        case displayOrder = "display_order"
    }
}

// MARK: - Supabase Source Response
struct SupabaseSource: Codable {
    let id: String
    let name: String
    let slug: String
    let feedUrl: String
    let websiteUrl: String?
    let logoUrl: String?
    let categoryId: String?
    let isActive: Bool

    enum CodingKeys: String, CodingKey {
        case id, name, slug
        case feedUrl = "feed_url"
        case websiteUrl = "website_url"
        case logoUrl = "logo_url"
        case categoryId = "category_id"
        case isActive = "is_active"
    }
}
```

## Step 4: Create SupabaseNewsService

Create `Pulse/Home/API/SupabaseNewsService.swift`:

```swift
import Foundation
import Combine
import Supabase

final class SupabaseNewsService: NewsService {

    private let client: SupabaseClient
    private let decoder: JSONDecoder

    init() {
        self.client = SupabaseClient(
            supabaseURL: URL(string: SupabaseConfig.url)!,
            supabaseKey: SupabaseConfig.anonKey
        )

        self.decoder = JSONDecoder()
        self.decoder.dateDecodingStrategy = .iso8601
    }

    // MARK: - NewsService Protocol

    func fetchTopHeadlines(country: String, page: Int, pageSize: Int) -> AnyPublisher<[Article], Error> {
        let offset = (page - 1) * pageSize

        return Future { [weak self] promise in
            guard let self = self else {
                promise(.failure(NSError(domain: "SupabaseNewsService", code: -1)))
                return
            }

            Task {
                do {
                    let response: [SupabaseArticle] = try await self.client
                        .from("articles_with_source")
                        .select()
                        .order("published_at", ascending: false)
                        .range(from: offset, to: offset + pageSize - 1)
                        .execute()
                        .value

                    let articles = response.map { $0.toArticle() }
                    promise(.success(articles))
                } catch {
                    promise(.failure(error))
                }
            }
        }
        .eraseToAnyPublisher()
    }

    func fetchHeadlinesByCategory(category: NewsCategory, page: Int, pageSize: Int) -> AnyPublisher<[Article], Error> {
        let offset = (page - 1) * pageSize

        return Future { [weak self] promise in
            guard let self = self else {
                promise(.failure(NSError(domain: "SupabaseNewsService", code: -1)))
                return
            }

            Task {
                do {
                    let response: [SupabaseArticle] = try await self.client
                        .from("articles_with_source")
                        .select()
                        .eq("category_slug", value: category.rawValue)
                        .order("published_at", ascending: false)
                        .range(from: offset, to: offset + pageSize - 1)
                        .execute()
                        .value

                    let articles = response.map { $0.toArticle() }
                    promise(.success(articles))
                } catch {
                    promise(.failure(error))
                }
            }
        }
        .eraseToAnyPublisher()
    }

    func searchArticles(query: String, page: Int, pageSize: Int, sortBy: String?) -> AnyPublisher<[Article], Error> {
        let offset = (page - 1) * pageSize

        return Future { [weak self] promise in
            guard let self = self else {
                promise(.failure(NSError(domain: "SupabaseNewsService", code: -1)))
                return
            }

            Task {
                do {
                    // Use PostgreSQL full-text search
                    let response: [SupabaseArticle] = try await self.client
                        .from("articles_with_source")
                        .select()
                        .textSearch("search_vector", query: query, type: .websearch)
                        .order("published_at", ascending: false)
                        .range(from: offset, to: offset + pageSize - 1)
                        .execute()
                        .value

                    let articles = response.map { $0.toArticle() }
                    promise(.success(articles))
                } catch {
                    promise(.failure(error))
                }
            }
        }
        .eraseToAnyPublisher()
    }

    func fetchArticle(id: String) -> AnyPublisher<Article, Error> {
        return Future { [weak self] promise in
            guard let self = self else {
                promise(.failure(NSError(domain: "SupabaseNewsService", code: -1)))
                return
            }

            Task {
                do {
                    let response: [SupabaseArticle] = try await self.client
                        .from("articles_with_source")
                        .select()
                        .eq("id", value: id)
                        .limit(1)
                        .execute()
                        .value

                    guard let article = response.first else {
                        throw NSError(domain: "SupabaseNewsService", code: 404,
                                      userInfo: [NSLocalizedDescriptionKey: "Article not found"])
                    }

                    promise(.success(article.toArticle()))
                } catch {
                    promise(.failure(error))
                }
            }
        }
        .eraseToAnyPublisher()
    }

    func fetchBreakingNews() -> AnyPublisher<[Article], Error> {
        // Return the 5 most recent articles as "breaking"
        return Future { [weak self] promise in
            guard let self = self else {
                promise(.failure(NSError(domain: "SupabaseNewsService", code: -1)))
                return
            }

            Task {
                do {
                    let response: [SupabaseArticle] = try await self.client
                        .from("articles_with_source")
                        .select()
                        .order("published_at", ascending: false)
                        .limit(5)
                        .execute()
                        .value

                    let articles = response.map { $0.toArticle() }
                    promise(.success(articles))
                } catch {
                    promise(.failure(error))
                }
            }
        }
        .eraseToAnyPublisher()
    }
}
```

## Step 5: Update Service Registration

In `PulseSceneDelegate.swift`, swap the service:

```swift
// Before (Guardian API)
let newsService = CachingNewsService(wrapping: LiveNewsService())
serviceLocator.register(NewsService.self, instance: newsService)

// After (Supabase)
let newsService = SupabaseNewsService()
serviceLocator.register(NewsService.self, instance: newsService)
```

**Note:** Caching is optional with Supabase since there are no API rate limits. However, you can still wrap it in `CachingNewsService` for offline support and reduced bandwidth.

## Step 6: Update Article Model (if needed)

Your existing `Article` model may need a small update to handle the new ID format (UUID vs Guardian's path-based ID):

```swift
// In Article.swift, ensure id is String type
struct Article: Identifiable, Codable, Equatable {
    let id: String  // Works with both UUID and Guardian's path IDs
    // ... rest of properties
}
```

## Step 7: Handle Deep Links

Update `DeeplinkRouter.swift` to handle UUID-based article IDs:

```swift
// The article deeplink now uses UUID
// pulse://article?id=550e8400-e29b-41d4-a716-446655440000

case .article:
    if let articleID = components.queryItems?.first(where: { $0.name == "id" })?.value {
        coordinator.push(page: .articleDetail(articleID: articleID), in: .home)
    }
```

## Testing

### Unit Tests

Create a mock that conforms to `NewsService`:

```swift
final class MockSupabaseNewsService: NewsService {
    var articlesToReturn: [Article] = []
    var errorToThrow: Error?

    func fetchTopHeadlines(country: String, page: Int, pageSize: Int) -> AnyPublisher<[Article], Error> {
        if let error = errorToThrow {
            return Fail(error: error).eraseToAnyPublisher()
        }
        return Just(articlesToReturn)
            .setFailureType(to: Error.self)
            .eraseToAnyPublisher()
    }

    // ... implement other methods
}
```

### UI Tests

No changes needed - the UI tests should work the same since only the service implementation changed.

## Migration Checklist

- [ ] Add Supabase Swift SDK dependency
- [ ] Create `SupabaseConfig.swift`
- [ ] Create `SupabaseModels.swift`
- [ ] Create `SupabaseNewsService.swift`
- [ ] Update `PulseSceneDelegate.swift` service registration
- [ ] Add Supabase URL and anon key to Remote Config
- [ ] Test all news-related features
- [ ] Update deeplink handling for UUID article IDs
- [ ] Run full test suite
