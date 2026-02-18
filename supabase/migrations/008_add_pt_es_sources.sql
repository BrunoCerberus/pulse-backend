-- Migration: Add Portuguese and Spanish RSS sources
-- Run this AFTER 007_add_language_support.sql

DO $$
DECLARE
    world_id UUID;
    technology_id UUID;
    business_id UUID;
    sports_id UUID;
    science_id UUID;
    entertainment_id UUID;
BEGIN
    SELECT id INTO world_id FROM categories WHERE slug = 'world';
    SELECT id INTO technology_id FROM categories WHERE slug = 'technology';
    SELECT id INTO business_id FROM categories WHERE slug = 'business';
    SELECT id INTO sports_id FROM categories WHERE slug = 'sports';
    SELECT id INTO science_id FROM categories WHERE slug = 'science';
    SELECT id INTO entertainment_id FROM categories WHERE slug = 'entertainment';

    -- Portuguese Sources (pt)

    -- News
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Folha de S.Paulo', 'folha-de-spaulo', 'https://feeds.folha.uol.com.br/emcimadahora/rss091.xml', 'https://www.folha.uol.com.br', world_id, 'pt', true),
        ('G1 (Globo)', 'g1-globo', 'https://g1.globo.com/rss/g1/', 'https://g1.globo.com', world_id, 'pt', true),
        ('BBC Brasil', 'bbc-brasil', 'https://feeds.bbci.co.uk/portuguese/rss.xml', 'https://www.bbc.com/portuguese', world_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Technology
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Tecnoblog', 'tecnoblog', 'https://tecnoblog.net/feed/', 'https://tecnoblog.net', technology_id, 'pt', true),
        ('Olhar Digital', 'olhar-digital', 'https://olhardigital.com.br/rss', 'https://olhardigital.com.br', technology_id, 'pt', true),
        ('Canaltech', 'canaltech', 'https://canaltech.com.br/rss/', 'https://canaltech.com.br', technology_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Business
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('InfoMoney', 'infomoney', 'https://www.infomoney.com.br/feed/', 'https://www.infomoney.com.br', business_id, 'pt', true),
        ('Exame', 'exame', 'https://exame.com/feed/', 'https://exame.com', business_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Sports
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('ge (Globo Esporte)', 'ge-globo-esporte', 'https://ge.globo.com/rss/ge/', 'https://ge.globo.com', sports_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Science
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('G1 Ciencia e Saude', 'g1-ciencia-saude', 'https://g1.globo.com/rss/g1/ciencia-e-saude/', 'https://g1.globo.com/ciencia-e-saude/', science_id, 'pt', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Spanish Sources (es)

    -- News
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('El Pais', 'el-pais', 'https://feeds.elpais.com/mrss-s/pages/ep/site/elpais.com/portada', 'https://elpais.com', world_id, 'es', true),
        ('BBC Mundo', 'bbc-mundo', 'https://feeds.bbci.co.uk/mundo/rss.xml', 'https://www.bbc.com/mundo', world_id, 'es', true),
        ('El Mundo', 'el-mundo', 'https://e00-elmundo.uecdn.es/elmundo/rss/portada.xml', 'https://www.elmundo.es', world_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Technology
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Xataka', 'xataka', 'https://feeds.weblogssl.com/xataka2', 'https://www.xataka.com', technology_id, 'es', true),
        ('Hipertextual', 'hipertextual', 'https://hipertextual.com/feed', 'https://hipertextual.com', technology_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Business
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Expansion', 'expansion', 'https://e00-expansion.uecdn.es/rss/portada.xml', 'https://www.expansion.com', business_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Sports
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Marca', 'marca', 'https://e00-marca.uecdn.es/rss/portada.xml', 'https://www.marca.com', sports_id, 'es', true),
        ('AS', 'as-deportes', 'https://feeds.as.com/mrss-s/pages/as/site/as.com/portada', 'https://as.com', sports_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Science
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('Muy Interesante', 'muy-interesante', 'https://www.muyinteresante.es/feed', 'https://www.muyinteresante.es', science_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

    -- Entertainment
    INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active) VALUES
        ('SensaCine', 'sensacine', 'https://www.sensacine.com/rss/noticias.xml', 'https://www.sensacine.com', entertainment_id, 'es', true)
    ON CONFLICT (slug) DO NOTHING;

END $$;
