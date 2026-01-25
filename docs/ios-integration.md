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

---

## Podcast & Video Support

The backend supports podcasts and YouTube videos alongside articles. This section covers how to handle media content in the iOS app.

### API Media Fields

The `/api-articles` endpoint returns media fields by default:
- `media_type` - "podcast", "video", or null
- `media_url` - Direct URL to audio/video file (MP3, etc.)
- `media_duration` - Duration in seconds
- `media_mime_type` - MIME type (audio/mpeg, video/mp4, etc.)

You can also filter by media type:
```
GET /api-articles?media_type=eq.podcast  # Only podcasts
GET /api-articles?media_type=eq.video    # Only videos
GET /api-articles?media_type=is.null     # Only articles
```

### Content Types

The API returns a `media_type` field that indicates the content type:

| `media_type` | Description |
|--------------|-------------|
| `null` | Regular article (text content) |
| `"podcast"` | Audio content (podcast episode) |
| `"video"` | Video content (YouTube, etc.) |

### Updated SupabaseArticle Model

Update `SupabaseModels.swift` to include media fields:

```swift
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

    // Media fields (for podcasts and videos)
    let mediaType: String?       // "podcast", "video", or nil
    let mediaUrl: String?        // Direct URL to audio/video file
    let mediaDuration: Int?      // Duration in seconds
    let mediaMimeType: String?   // "audio/mpeg", "video/mp4", etc.

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
        case mediaType = "media_type"
        case mediaUrl = "media_url"
        case mediaDuration = "media_duration"
        case mediaMimeType = "media_mime_type"
    }
}
```

### ContentType Enum

Create a `ContentType` enum to handle different content types:

```swift
enum ContentType: String, Codable, CaseIterable {
    case article
    case podcast
    case video

    init(from mediaType: String?) {
        switch mediaType {
        case "podcast": self = .podcast
        case "video": self = .video
        default: self = .article
        }
    }

    var icon: String {
        switch self {
        case .article: return "doc.text"
        case .podcast: return "headphones"
        case .video: return "play.rectangle"
        }
    }
}
```

### Updated Article Model

Extend your `Article` model to support media:

```swift
struct Article: Identifiable, Codable, Equatable {
    let id: String
    let title: String
    let description: String?
    let content: String?
    let url: String
    let imageURL: String?
    let publishedAt: Date
    let source: Source
    let author: String?
    let category: NewsCategory

    // Media properties
    let contentType: ContentType
    let mediaURL: String?
    let mediaDuration: Int?

    /// Formatted duration string (e.g., "1:23:45")
    var formattedDuration: String? {
        guard let duration = mediaDuration, duration > 0 else { return nil }

        let hours = duration / 3600
        let minutes = (duration % 3600) / 60
        let seconds = duration % 60

        if hours > 0 {
            return String(format: "%d:%02d:%02d", hours, minutes, seconds)
        } else {
            return String(format: "%d:%02d", minutes, seconds)
        }
    }

    var isMedia: Bool {
        contentType != .article
    }
}
```

### Updated toArticle() Conversion

Update the conversion method in `SupabaseArticle`:

```swift
extension SupabaseArticle {
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
            category: categorySlug.flatMap { NewsCategory(rawValue: $0) } ?? .world,
            contentType: ContentType(from: mediaType),
            mediaURL: mediaUrl,
            mediaDuration: mediaDuration
        )
    }
}
```

### Fetching Podcasts and Videos

Add methods to fetch media content:

```swift
extension SupabaseNewsService {

    /// Fetch podcast episodes
    func fetchPodcasts(page: Int, pageSize: Int) -> AnyPublisher<[Article], Error> {
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
                        .eq("category_slug", value: "podcasts")
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

    /// Fetch videos
    func fetchVideos(page: Int, pageSize: Int) -> AnyPublisher<[Article], Error> {
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
                        .eq("category_slug", value: "videos")
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

    /// Fetch all media (podcasts + videos)
    func fetchMedia(page: Int, pageSize: Int) -> AnyPublisher<[Article], Error> {
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
                        .not("media_type", operator: .is, value: "null")
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
}
```

### UI Components

#### Media Card View

Create a reusable card that displays content type indicator and duration:

```swift
struct MediaCardView: View {
    let article: Article

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            // Thumbnail with overlay
            ZStack(alignment: .bottomLeading) {
                AsyncImage(url: URL(string: article.imageURL ?? "")) { image in
                    image.resizable().aspectRatio(contentMode: .fill)
                } placeholder: {
                    Color.gray.opacity(0.3)
                }
                .frame(height: 180)
                .clipped()

                // Content type badge
                HStack(spacing: 4) {
                    Image(systemName: article.contentType.icon)
                    if let duration = article.formattedDuration {
                        Text(duration)
                    }
                }
                .font(.caption.bold())
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(.ultraThinMaterial)
                .cornerRadius(4)
                .padding(8)
            }

            // Title and metadata
            Text(article.title)
                .font(.headline)
                .lineLimit(2)

            Text(article.source.name)
                .font(.caption)
                .foregroundColor(.secondary)
        }
    }
}
```

#### Handling Media Playback

For podcasts and videos, you have several options:

**Option 1: Open in Safari/YouTube (Simplest)**
```swift
func openMedia(_ article: Article) {
    guard let url = URL(string: article.url) else { return }
    UIApplication.shared.open(url)
}
```

**Option 2: In-app Audio Player (Podcasts)**
```swift
import AVFoundation

class AudioPlayerManager: ObservableObject {
    private var player: AVPlayer?
    @Published var isPlaying = false
    @Published var currentTime: Double = 0
    @Published var duration: Double = 0

    func play(url: String) {
        guard let mediaURL = URL(string: url) else { return }
        player = AVPlayer(url: mediaURL)
        player?.play()
        isPlaying = true
    }

    func pause() {
        player?.pause()
        isPlaying = false
    }

    func toggle() {
        isPlaying ? pause() : player?.play()
        isPlaying.toggle()
    }
}
```

**Option 3: In-app Video Player**
```swift
import AVKit

struct VideoPlayerView: View {
    let url: String

    var body: some View {
        if let videoURL = URL(string: url) {
            VideoPlayer(player: AVPlayer(url: videoURL))
                .edgesIgnoringSafeArea(.all)
        }
    }
}
```

### Updated NewsCategory Enum

Add podcasts and videos to your category enum:

```swift
enum NewsCategory: String, CaseIterable, Codable {
    case world
    case technology
    case business
    case sports
    case entertainment
    case science
    case health
    case politics
    case podcasts    // New
    case videos      // New

    var displayName: String {
        switch self {
        case .world: return "World"
        case .technology: return "Technology"
        case .business: return "Business"
        case .sports: return "Sports"
        case .entertainment: return "Entertainment"
        case .science: return "Science"
        case .health: return "Health"
        case .politics: return "Politics"
        case .podcasts: return "Podcasts"
        case .videos: return "Videos"
        }
    }

    var icon: String {
        switch self {
        case .podcasts: return "headphones"
        case .videos: return "play.rectangle"
        default: return "newspaper"
        }
    }
}
```

### Media Checklist

- [ ] Update `SupabaseArticle` model with media fields
- [ ] Create `ContentType` enum
- [ ] Update `Article` model with media properties
- [ ] Add `formattedDuration` computed property
- [ ] Add fetch methods for podcasts/videos
- [ ] Create `MediaCardView` component
- [ ] Add podcasts/videos to `NewsCategory` enum
- [ ] Implement media playback (Safari, AVPlayer, or AVKit)
- [ ] Add media tab or section to home screen
- [ ] Test with real podcast and video content
